package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sheguard/backend/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const fcmMessagingScope = "https://www.googleapis.com/auth/firebase.messaging"

var (
	fcmTokenSource     oauth2.TokenSource
	fcmTokenSourceErr  error
	fcmTokenSourceOnce sync.Once
)

// FCMNotification represents a push notification payload.
type FCMNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// FCMData represents custom data sent with the notification.
type FCMData map[string]string

// SendPushNotification sends an FCM HTTP v1 notification to a specific device token.
func SendPushNotification(fcmToken string, notification FCMNotification, data FCMData) error {
	cfg := config.GetSupabaseConfig()
	if cfg == nil {
		return fmt.Errorf("server config not initialized")
	}

	projectID := strings.TrimSpace(cfg.FirebaseProjectID)
	if projectID == "" {
		return fmt.Errorf("FIREBASE_PROJECT_ID not configured")
	}

	fcmToken = strings.TrimSpace(fcmToken)
	if fcmToken == "" {
		return fmt.Errorf("empty fcm token")
	}

	tokenSource, err := getFCMTokenSource(cfg)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"token": fcmToken,
			"notification": map[string]string{
				"title": notification.Title,
				"body":  notification.Body,
			},
			"data": data,
			"android": map[string]interface{}{
				"priority": "HIGH",
				"notification": map[string]string{
					"sound": "default",
				},
			},
			"apns": map[string]interface{}{
				"payload": map[string]interface{}{
					"aps": map[string]string{
						"sound": "default",
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", projectID)
	client := oauth2.NewClient(context.Background(), tokenSource)
	req, err := http.NewRequestWithContext(context.Background(), "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("FCM HTTP v1 request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("FCM HTTP v1 returned status %d: %s", resp.StatusCode, string(respBody))
	}

	if len(fcmToken) > 12 {
		log.Printf("FCM: notification sent to token %s...%s", fcmToken[:8], fcmToken[len(fcmToken)-4:])
	} else {
		log.Printf("FCM: notification sent to token (len=%d)", len(fcmToken))
	}
	return nil
}

// SendPushToUser sends a push notification to a user by looking up their FCM token.
func SendPushToUser(userID string, notification FCMNotification, data FCMData) error {
	cfg := config.GetSupabaseConfig()
	fcmToken, err := getUserFCMToken(cfg, userID)
	if err != nil {
		return err
	}
	if fcmToken == "" {
		log.Printf("FCM: no token found for user %s", userID)
		return nil
	}

	return SendPushNotification(fcmToken, notification, data)
}

func getFCMTokenSource(cfg *config.Config) (oauth2.TokenSource, error) {
	fcmTokenSourceOnce.Do(func() {
		credentialsPath := strings.TrimSpace(cfg.GoogleApplicationCreds)
		if credentialsPath != "" {
			if !filepath.IsAbs(credentialsPath) {
				if absPath, err := filepath.Abs(credentialsPath); err == nil {
					credentialsPath = absPath
				}
			}
			if _, err := os.Stat(credentialsPath); err != nil {
				fcmTokenSourceErr = fmt.Errorf("service account file not found at %s: %w", credentialsPath, err)
				return
			}
			if err := os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath); err != nil {
				fcmTokenSourceErr = fmt.Errorf("failed setting GOOGLE_APPLICATION_CREDENTIALS: %w", err)
				return
			}
		}

		tokenSource, err := google.DefaultTokenSource(context.Background(), fcmMessagingScope)
		if err != nil {
			fcmTokenSourceErr = fmt.Errorf("failed creating FCM token source: %w", err)
			return
		}
		fcmTokenSource = tokenSource
	})

	if fcmTokenSourceErr != nil {
		return nil, fcmTokenSourceErr
	}
	if fcmTokenSource == nil {
		return nil, fmt.Errorf("failed creating FCM token source")
	}
	return fcmTokenSource, nil
}

func getUserFCMToken(cfg *config.Config, userID string) (string, error) {
	if cfg == nil || cfg.SupabaseURL == "" || cfg.SupabaseServiceRoleKey == "" {
		return "", fmt.Errorf("supabase service role config missing")
	}

	baseURL := strings.TrimRight(cfg.SupabaseURL, "/") + "/rest/v1/profiles"
	params := url.Values{}
	params.Set("user_id", "eq."+userID)
	params.Set("select", "fcm_token")
	params.Set("limit", "1")

	req, err := http.NewRequest("GET", baseURL+"?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("apikey", cfg.SupabaseServiceRoleKey)
	req.Header.Set("Authorization", "Bearer "+cfg.SupabaseServiceRoleKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("supabase profile query failed (%d): %s", resp.StatusCode, string(body))
	}

	var rows []struct {
		FCMToken string `json:"fcm_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return strings.TrimSpace(rows[0].FCMToken), nil
}
