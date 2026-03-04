package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
	"github.com/sheguard/backend/services"
)

// TriggerSOS initiates an SOS event.
func TriggerSOS(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()

	var input struct {
		Type string  `json:"type"` // "timer" or "instant"
		Lat  float64 `json:"lat"`
		Lng  float64 `json:"lng"`
	}
	if err := c.Bind(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if input.Type != "timer" && input.Type != "instant" {
		input.Type = "timer"
	}

	// Generate unique channel name
	channelName := fmt.Sprintf("sos_%s_%d", userID, time.Now().UnixMilli())

	// Generate Agora token (10 min max)
	agoraToken, err := services.GenerateAgoraToken(channelName, 0, services.MaxSOSDurationSeconds)
	if err != nil {
		log.Printf("SOS: Agora token generation failed: %v", err)
		// Continue without Agora — location-only SOS
	}

	// Create SOS event in AppWrite
	sosData := map[string]interface{}{
		"triggeredBy":  userID,
		"type":         input.Type,
		"status":       "active",
		"agoraChannel": channelName,
		"startedAt":    time.Now().UTC().Format(time.RFC3339),
	}
	sosDoc, err := createAppWriteDocument(cfg, "sos_events", sosData)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create SOS event"})
	}

	sosEventID := ""
	if id, ok := sosDoc["$id"].(string); ok {
		sosEventID = id
	}

	// Get triggering user's profile for the notification
	triggerProfile, _ := queryProfileByUserID(cfg, userID)
	triggerName := "Someone"
	if triggerProfile != nil {
		if name, ok := triggerProfile["name"].(string); ok {
			triggerName = name
		}
	}

	// Get all contacts and notify them
	contacts, _ := queryAppWriteDocuments(cfg, "contacts", "ownerId", userID)
	notifiedCount := 0

	for _, contact := range contacts {
		contactUserID, _ := contact["contactUserId"].(string)
		contactType, _ := contact["type"].(string)

		// Build notification data
		notifData := services.FCMData{
			"type":         "sos_alert",
			"sosEventId":   sosEventID,
			"triggeredBy":  userID,
			"triggerName":  triggerName,
			"contactType":  contactType,
			"lat":          fmt.Sprintf("%f", input.Lat),
			"lng":          fmt.Sprintf("%f", input.Lng),
			"channelName":  channelName,
			"agoraAppId":   cfg.AgoraAppID,
		}

		// Only trusted contacts get Agora token for video/voice
		if strings.EqualFold(contactType, "trusted") && agoraToken != "" {
			notifData["agoraToken"] = agoraToken
		}

		// Send push notification
		go services.SendPushToUser(contactUserID, services.FCMNotification{
			Title: "🆘 SOS Alert!",
			Body:  fmt.Sprintf("%s needs help! Tap to view their location.", triggerName),
		}, notifData)

		// Also send via WebSocket for in-app users
		go BroadcastToUser(contactUserID, map[string]interface{}{
			"type":        "sos_alert",
			"sosEventId":  sosEventID,
			"triggeredBy": userID,
			"triggerName": triggerName,
			"contactType": contactType,
			"lat":         input.Lat,
			"lng":         input.Lng,
			"channelName": channelName,
			"agoraAppId":  cfg.AgoraAppID,
			"agoraToken":  func() string { if strings.EqualFold(contactType, "trusted") { return agoraToken }; return "" }(),
		})

		notifiedCount++
	}

	log.Printf("SOS: triggered by %s (%s), notified %d contacts, channel=%s", userID, input.Type, notifiedCount, channelName)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"sosEventId":  sosEventID,
		"channelName": channelName,
		"agoraToken":  agoraToken,
		"agoraAppId":  cfg.AgoraAppID,
		"notified":    notifiedCount,
		"maxDuration": services.MaxSOSDurationSeconds,
	})
}

// ResolveSOS ends an active SOS event and cleans up resources.
func ResolveSOS(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()

	var input struct {
		SOSEventID string `json:"sosEventId"`
	}
	if err := c.Bind(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	// Update SOS event status to resolved
	_, err := updateAppWriteDocument(cfg, "sos_events", input.SOSEventID, map[string]interface{}{
		"status":  "resolved",
		"endedAt": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to resolve SOS event"})
	}

	// Notify all contacts that SOS is resolved
	contacts, _ := queryAppWriteDocuments(cfg, "contacts", "ownerId", userID)
	for _, contact := range contacts {
		contactUserID, _ := contact["contactUserId"].(string)
		go BroadcastToUser(contactUserID, map[string]interface{}{
			"type":       "sos_resolved",
			"sosEventId": input.SOSEventID,
			"resolvedBy": userID,
		})
		go services.SendPushToUser(contactUserID, services.FCMNotification{
			Title: "✅ SOS Resolved",
			Body:  "The SOS alert has been resolved. Everyone is safe.",
		}, services.FCMData{"type": "sos_resolved", "sosEventId": input.SOSEventID})
	}

	log.Printf("SOS: resolved by %s, event=%s", userID, input.SOSEventID)
	return c.JSON(http.StatusOK, map[string]string{"message": "SOS resolved"})
}
