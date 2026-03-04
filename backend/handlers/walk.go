package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
	"github.com/sheguard/backend/services"
)

// InviteWalk creates a walk session and notifies the selected contacts.
func InviteWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()

	var input struct {
		ContactIDs []string `json:"contactIds"` // list of contactUserIds to invite
	}
	if err := c.Bind(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if len(input.ContactIDs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "at least one contact is required"})
	}

	// Check if user already has an active/pending walk
	existing, _ := queryAppWriteDocuments(cfg, "walk_sessions", "requesterId", userID)
	for _, doc := range existing {
		status, _ := doc["status"].(string)
		if status == "pending" || status == "active" {
			return c.JSON(http.StatusConflict, map[string]string{"error": "you already have an active walk session"})
		}
	}

	// Get requester's profile for notification
	requesterProfile, _ := queryProfileByUserID(cfg, userID)
	requesterName := "Someone"
	if requesterProfile != nil {
		if name, ok := requesterProfile["name"].(string); ok {
			requesterName = name
		}
	}

	// Create walk session document
	walkData := map[string]interface{}{
		"requesterId": userID,
		"accepterId":  "",
		"invitedIds":  input.ContactIDs,
		"status":      "pending",
		"startedAt":   time.Now().UTC().Format(time.RFC3339),
		"endedAt":     "",
	}

	walkDoc, err := createAppWriteDocument(cfg, "walk_sessions", walkData)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create walk session"})
	}

	walkID := ""
	if id, ok := walkDoc["$id"].(string); ok {
		walkID = id
	}

	// Notify all invited contacts
	notifiedCount := 0
	for _, contactUserID := range input.ContactIDs {
		notifData := services.FCMData{
			"type":          "walk_invite",
			"walkId":        walkID,
			"requesterId":   userID,
			"requesterName": requesterName,
		}

		// Send push notification
		go services.SendPushToUser(contactUserID, services.FCMNotification{
			Title: "🚶 Walk With Me",
			Body:  fmt.Sprintf("%s wants you to walk with them!", requesterName),
		}, notifData)

		// Send via WebSocket for in-app users
		go BroadcastToUser(contactUserID, map[string]interface{}{
			"type":          "walk_invite",
			"walkId":        walkID,
			"requesterId":   userID,
			"requesterName": requesterName,
		})

		notifiedCount++
	}

	log.Printf("Walk: invite by %s (%s), invited %d contacts, walkId=%s", userID, requesterName, notifiedCount, walkID)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"walkId":   walkID,
		"status":   "pending",
		"notified": notifiedCount,
	})
}

// AcceptWalk accepts a walk invitation and starts the walk session.
func AcceptWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()
	walkID := c.Param("id")

	if walkID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "walk ID is required"})
	}

	// Update walk session: set accepter, status → active
	_, err := updateAppWriteDocument(cfg, "walk_sessions", walkID, map[string]interface{}{
		"accepterId": userID,
		"status":     "active",
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to accept walk"})
	}

	// Get accepter's name for notification
	accepterProfile, _ := queryProfileByUserID(cfg, userID)
	accepterName := "Someone"
	if accepterProfile != nil {
		if name, ok := accepterProfile["name"].(string); ok {
			accepterName = name
		}
	}

	// Get walk session to find requester
	walkDoc, err := getAppWriteDocument(cfg, "walk_sessions", walkID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get walk session"})
	}

	requesterID, _ := walkDoc["requesterId"].(string)

	// Notify requester via FCM + WS
	go services.SendPushToUser(requesterID, services.FCMNotification{
		Title: "✅ Walk Accepted!",
		Body:  fmt.Sprintf("%s is now walking with you!", accepterName),
	}, services.FCMData{
		"type":         "walk_accepted",
		"walkId":       walkID,
		"accepterId":   userID,
		"accepterName": accepterName,
	})

	go BroadcastToUser(requesterID, map[string]interface{}{
		"type":         "walk_accepted",
		"walkId":       walkID,
		"accepterId":   userID,
		"accepterName": accepterName,
	})

	log.Printf("Walk: accepted by %s (%s), walkId=%s", userID, accepterName, walkID)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "walk accepted",
		"walkId":  walkID,
	})
}

// RejectWalk rejects a walk invitation.
func RejectWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()
	walkID := c.Param("id")

	if walkID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "walk ID is required"})
	}

	// Get walk session to find requester
	walkDoc, err := getAppWriteDocument(cfg, "walk_sessions", walkID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get walk session"})
	}

	requesterID, _ := walkDoc["requesterId"].(string)

	// Get rejector's name
	rejectorProfile, _ := queryProfileByUserID(cfg, userID)
	rejectorName := "Someone"
	if rejectorProfile != nil {
		if name, ok := rejectorProfile["name"].(string); ok {
			rejectorName = name
		}
	}

	// Check invited IDs — remove this user from the list
	invitedIDs := extractStringArray(walkDoc, "invitedIds")
	remainingIDs := make([]string, 0)
	for _, id := range invitedIDs {
		if id != userID {
			remainingIDs = append(remainingIDs, id)
		}
	}

	// If no one left to accept, cancel the walk
	newStatus := "pending"
	if len(remainingIDs) == 0 {
		newStatus = "cancelled"
	}

	_, err = updateAppWriteDocument(cfg, "walk_sessions", walkID, map[string]interface{}{
		"invitedIds": remainingIDs,
		"status":     newStatus,
		"endedAt": func() string {
			if newStatus == "cancelled" {
				return time.Now().UTC().Format(time.RFC3339)
			}
			return ""
		}(),
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to reject walk"})
	}

	// Notify requester
	go BroadcastToUser(requesterID, map[string]interface{}{
		"type":         "walk_rejected",
		"walkId":       walkID,
		"rejectorId":   userID,
		"rejectorName": rejectorName,
		"newStatus":    newStatus,
	})

	log.Printf("Walk: rejected by %s (%s), walkId=%s, remaining=%d", userID, rejectorName, walkID, len(remainingIDs))

	return c.JSON(http.StatusOK, map[string]string{
		"message": "walk rejected",
		"walkId":  walkID,
	})
}

// CompleteWalk marks a walk session as completed.
func CompleteWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()
	walkID := c.Param("id")

	if walkID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "walk ID is required"})
	}

	// Update walk session
	_, err := updateAppWriteDocument(cfg, "walk_sessions", walkID, map[string]interface{}{
		"status":  "completed",
		"endedAt": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to complete walk"})
	}

	// Get walk session to notify the other party
	walkDoc, _ := getAppWriteDocument(cfg, "walk_sessions", walkID)
	requesterID, _ := walkDoc["requesterId"].(string)
	accepterID, _ := walkDoc["accepterId"].(string)

	// Get the completer's name
	completerProfile, _ := queryProfileByUserID(cfg, userID)
	completerName := "Someone"
	if completerProfile != nil {
		if name, ok := completerProfile["name"].(string); ok {
			completerName = name
		}
	}

	// Notify the other party
	otherUserID := requesterID
	if userID == requesterID {
		otherUserID = accepterID
	}

	if otherUserID != "" {
		go services.SendPushToUser(otherUserID, services.FCMNotification{
			Title: "🏁 Walk Completed",
			Body:  fmt.Sprintf("%s has arrived safely!", completerName),
		}, services.FCMData{
			"type":   "walk_completed",
			"walkId": walkID,
		})

		go BroadcastToUser(otherUserID, map[string]interface{}{
			"type":          "walk_completed",
			"walkId":        walkID,
			"completedBy":   userID,
			"completerName": completerName,
		})
	}

	log.Printf("Walk: completed by %s, walkId=%s", userID, walkID)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "walk completed",
		"walkId":  walkID,
	})
}

// CancelWalk cancels a pending or active walk session.
func CancelWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()
	walkID := c.Param("id")

	if walkID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "walk ID is required"})
	}

	// Update status to cancelled
	_, err := updateAppWriteDocument(cfg, "walk_sessions", walkID, map[string]interface{}{
		"status":  "cancelled",
		"endedAt": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to cancel walk"})
	}

	// Notify relevant parties
	walkDoc, _ := getAppWriteDocument(cfg, "walk_sessions", walkID)
	requesterID, _ := walkDoc["requesterId"].(string)
	accepterID, _ := walkDoc["accepterId"].(string)

	cancellerProfile, _ := queryProfileByUserID(cfg, userID)
	cancellerName := "Someone"
	if cancellerProfile != nil {
		if name, ok := cancellerProfile["name"].(string); ok {
			cancellerName = name
		}
	}

	// Notify other party
	otherUserID := requesterID
	if userID == requesterID {
		otherUserID = accepterID
	}

	if otherUserID != "" {
		go BroadcastToUser(otherUserID, map[string]interface{}{
			"type":          "walk_cancelled",
			"walkId":        walkID,
			"cancelledBy":   userID,
			"cancellerName": cancellerName,
		})

		go services.SendPushToUser(otherUserID, services.FCMNotification{
			Title: "❌ Walk Cancelled",
			Body:  fmt.Sprintf("%s cancelled the walk session.", cancellerName),
		}, services.FCMData{
			"type":   "walk_cancelled",
			"walkId": walkID,
		})
	}

	// Also notify all invited contacts if walk was pending
	invitedIDs := extractStringArray(walkDoc, "invitedIds")
	for _, invitedID := range invitedIDs {
		if invitedID != userID {
			go BroadcastToUser(invitedID, map[string]interface{}{
				"type":          "walk_cancelled",
				"walkId":        walkID,
				"cancelledBy":   userID,
				"cancellerName": cancellerName,
			})
		}
	}

	log.Printf("Walk: cancelled by %s, walkId=%s", userID, walkID)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "walk cancelled",
		"walkId":  walkID,
	})
}

// GetActiveWalk returns the user's current active or pending walk session.
func GetActiveWalk(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()

	// Check as requester
	docs, _ := queryAppWriteDocuments(cfg, "walk_sessions", "requesterId", userID)
	for _, doc := range docs {
		status, _ := doc["status"].(string)
		if status == "pending" || status == "active" {
			doc["role"] = "requester"
			return c.JSON(http.StatusOK, doc)
		}
	}

	// Check as accepter
	docs, _ = queryAppWriteDocuments(cfg, "walk_sessions", "accepterId", userID)
	for _, doc := range docs {
		status, _ := doc["status"].(string)
		if status == "active" {
			doc["role"] = "accepter"
			return c.JSON(http.StatusOK, doc)
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"active": false,
	})
}

// ── helpers ──

func getAppWriteDocument(cfg *config.Config, collectionID, docID string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/databases/%s/collections/%s/documents/%s",
		cfg.AppWriteEndpoint, cfg.AppWriteDBID, collectionID, docID)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-Appwrite-Project", cfg.AppWriteProjectID)
	req.Header.Set("X-Appwrite-Key", cfg.AppWriteAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var doc map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func extractStringArray(doc map[string]interface{}, key string) []string {
	result := make([]string, 0)
	if arr, ok := doc[key].([]interface{}); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
	}
	return result
}
