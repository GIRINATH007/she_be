package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
)

// SaveLocation persists the caller's current location.
func SaveLocation(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()

	var input struct {
		Lat      float64 `json:"lat"`
		Lng      float64 `json:"lng"`
		Accuracy float64 `json:"accuracy"`
	}
	if err := c.Bind(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	// Fetch the user's name for the location record
	name := ""
	profile, _ := queryProfileByUserID(cfg, userID)
	if profile != nil {
		if n, ok := profile["name"].(string); ok {
			name = n
		}
	}

	if err := upsertSupabaseDocument(cfg, "user_locations", map[string]interface{}{
		"userId":    userID,
		"lat":       input.Lat,
		"lng":       input.Lng,
		"accuracy":  input.Accuracy,
		"name":      name,
		"updatedAt": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		log.Printf("Location: upsert failed for %s: %v", userID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save location"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "location saved"})
}

// GetContactLocations returns the last known location of all contacts.
func GetContactLocations(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()

	// Collect contact user IDs from both directions
	peerIDs := make(map[string]bool)

	contacts, _ := querySupabaseDocuments(cfg, "contacts", "ownerId", userID)
	for _, c := range contacts {
		if id, _ := c["contactUserId"].(string); id != "" {
			peerIDs[id] = true
		}
	}

	reverseContacts, _ := querySupabaseDocuments(cfg, "contacts", "contactUserId", userID)
	for _, c := range reverseContacts {
		if id, _ := c["ownerId"].(string); id != "" {
			peerIDs[id] = true
		}
	}

	// Fetch each contact's last known location
	locations := make([]map[string]interface{}, 0)
	for peerID := range peerIDs {
		docs, err := querySupabaseDocuments(cfg, "user_locations", "userId", peerID)
		if err != nil || len(docs) == 0 {
			continue
		}
		loc := docs[0]
		lat, _ := loc["lat"].(float64)
		lng, _ := loc["lng"].(float64)
		accuracy, _ := loc["accuracy"].(float64)
		name, _ := loc["name"].(string)
		updatedAt, _ := loc["updatedAt"].(string)

		locations = append(locations, map[string]interface{}{
			"userId":    peerID,
			"lat":       lat,
			"lng":       lng,
			"accuracy":  accuracy,
			"name":      name,
			"updatedAt": updatedAt,
		})
	}

	log.Printf("Location: returned %d contact locations for %s", len(locations), userID)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"locations": locations,
		"total":     len(locations),
	})
}

// PersistLocationAsync saves a location update to the database (called from WS handler).
func PersistLocationAsync(cfg *config.Config, userID, name string, lat, lng, accuracy float64) {
	if err := upsertSupabaseDocument(cfg, "user_locations", map[string]interface{}{
		"userId":    userID,
		"lat":       lat,
		"lng":       lng,
		"accuracy":  accuracy,
		"name":      name,
		"updatedAt": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		log.Printf("Location: async persist failed for %s: %v", userID, err)
	}
}

// formatFloat is a helper for formatting floats in notification data.
func formatFloat(f float64) string {
	return fmt.Sprintf("%f", f)
}
