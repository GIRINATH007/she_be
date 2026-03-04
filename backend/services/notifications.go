package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/sheguard/backend/config"
)

// FCMNotification represents a push notification payload.
type FCMNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// FCMData represents custom data sent with the notification.
type FCMData map[string]string

// SendPushNotification sends an FCM notification to a specific device token.
func SendPushNotification(fcmToken string, notification FCMNotification, data FCMData) error {
	cfg := config.GetAppWriteConfig()
	if cfg.FCMServerKey == "" {
		log.Println("FCM: server key not configured, skipping notification")
		return nil
	}

	payload := map[string]interface{}{
		"to": fcmToken,
		"notification": map[string]string{
			"title": notification.Title,
			"body":  notification.Body,
			"sound": "default",
		},
		"data":     data,
		"priority": "high",
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://fcm.googleapis.com/fcm/send", bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "key="+cfg.FCMServerKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("FCM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("FCM returned status %d", resp.StatusCode)
	}

	log.Printf("FCM: notification sent to token %s...%s", fcmToken[:8], fcmToken[len(fcmToken)-4:])
	return nil
}

// SendPushToUser sends a push notification to a user by looking up their FCM token.
func SendPushToUser(userID string, notification FCMNotification, data FCMData) error {
	cfg := config.GetAppWriteConfig()

	// Get user's FCM token from profiles collection
	url := fmt.Sprintf("%s/databases/%s/collections/profiles/documents?queries[]=equal(\"userId\",[\"%s\"])",
		cfg.AppWriteEndpoint, cfg.AppWriteDBID, userID)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-Appwrite-Project", cfg.AppWriteProjectID)
	req.Header.Set("X-Appwrite-Key", cfg.AppWriteAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Documents []struct {
			FCMToken string `json:"fcmToken"`
		} `json:"documents"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Documents) == 0 || result.Documents[0].FCMToken == "" {
		log.Printf("FCM: no token found for user %s", userID)
		return nil
	}

	return SendPushNotification(result.Documents[0].FCMToken, notification, data)
}
