package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
	appwritemw "github.com/sheguard/backend/middleware"
	"github.com/sheguard/backend/models"
	"github.com/sheguard/backend/services"
)

// GetProfile returns the current user's profile from AppWrite.
func GetProfile(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()

	// Query AppWrite for profile by userId
	doc, err := queryProfileByUserID(cfg, userID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "profile not found",
		})
	}

	return c.JSON(http.StatusOK, doc)
}

// UpdateProfile creates or updates the current user's profile in AppWrite.
func UpdateProfile(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()

	var input struct {
		Name        string `json:"name"`
		Phone       string `json:"phone"`
		BloodGroup  string `json:"bloodGroup"`
		Allergies   string `json:"allergies"`
		Medications string `json:"medications"`
		PinHash     string `json:"pinHash"`
		FCMToken    string `json:"fcmToken"`
	}

	if err := c.Bind(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	// Validate required fields
	if strings.TrimSpace(input.Name) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}
	if strings.TrimSpace(input.Phone) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "phone is required"})
	}

	// Build the data payload with sanitized inputs
	data := map[string]interface{}{
		"userId":      userID,
		"name":        appwritemw.SanitizeString(input.Name),
		"phone":       appwritemw.SanitizeString(input.Phone),
		"bloodGroup":  appwritemw.SanitizeString(input.BloodGroup),
		"allergies":   appwritemw.SanitizeString(input.Allergies),
		"medications": appwritemw.SanitizeString(input.Medications),
	}
	if input.PinHash != "" {
		// Hash the PIN using bcrypt before storing
		hashed, err := services.HashPIN(input.PinHash)
		if err == nil {
			data["pinHash"] = hashed
		}
	}
	if input.FCMToken != "" {
		data["fcmToken"] = input.FCMToken
	}

	// Check if profile already exists
	existingDoc, err := queryProfileByUserID(cfg, userID)
	if err == nil && existingDoc != nil {
		// Update existing document
		docID := existingDoc["$id"].(string)
		result, err := updateAppWriteDocument(cfg, "profiles", docID, data)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update profile"})
		}
		return c.JSON(http.StatusOK, result)
	}

	// Create new document
	result, err := createAppWriteDocument(cfg, "profiles", data)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create profile: " + err.Error()})
	}
	return c.JSON(http.StatusCreated, result)
}

// ---------- AppWrite HTTP helpers ----------

func queryProfileByUserID(cfg *config.Config, userID string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/databases/%s/collections/profiles/documents?queries[]=equal(\"userId\",[\"%s\"])",
		cfg.AppWriteEndpoint, cfg.AppWriteDBID, userID)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-Appwrite-Project", cfg.AppWriteProjectID)
	req.Header.Set("X-Appwrite-Key", cfg.AppWriteAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Documents []map[string]interface{} `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Documents) == 0 {
		return nil, fmt.Errorf("not found")
	}

	return result.Documents[0], nil
}

func createAppWriteDocument(cfg *config.Config, collectionID string, data map[string]interface{}) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/databases/%s/collections/%s/documents",
		cfg.AppWriteEndpoint, cfg.AppWriteDBID, collectionID)

	payload := map[string]interface{}{
		"documentId":  "unique()",
		"data":        data,
		"permissions": []string{},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, strings.NewReader(string(body)))
	req.Header.Set("X-Appwrite-Project", cfg.AppWriteProjectID)
	req.Header.Set("X-Appwrite-Key", cfg.AppWriteAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("AppWrite error (%d): %s", resp.StatusCode, string(respBody))
	}

	var doc map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&doc)
	return doc, nil
}

func updateAppWriteDocument(cfg *config.Config, collectionID, docID string, data map[string]interface{}) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/databases/%s/collections/%s/documents/%s",
		cfg.AppWriteEndpoint, cfg.AppWriteDBID, collectionID, docID)

	payload := map[string]interface{}{
		"data": data,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("PATCH", url, strings.NewReader(string(body)))
	req.Header.Set("X-Appwrite-Project", cfg.AppWriteProjectID)
	req.Header.Set("X-Appwrite-Key", cfg.AppWriteAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("AppWrite error (%d): %s", resp.StatusCode, string(respBody))
	}

	var doc map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&doc)
	return doc, nil
}

// Ensure models import is used (for future type safety)
var _ = models.Profile{}
