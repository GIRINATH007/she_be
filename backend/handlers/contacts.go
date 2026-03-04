package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
)

// GetContacts lists all contacts for the current user.
func GetContacts(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()

	docs, err := queryAppWriteDocuments(cfg, "contacts", "ownerId", userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch contacts"})
	}

	// Enrich contacts with profile data
	enriched := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		contactUserID, _ := doc["contactUserId"].(string)
		profile, _ := queryProfileByUserID(cfg, contactUserID)

		entry := map[string]interface{}{
			"$id":           doc["$id"],
			"contactUserId": contactUserID,
			"type":          doc["type"],
			"status":        doc["status"],
		}
		if profile != nil {
			entry["name"] = profile["name"]
			entry["phone"] = profile["phone"]
			entry["avatarUrl"] = profile["avatarUrl"]
		}
		enriched = append(enriched, entry)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"contacts": enriched,
		"total":    len(enriched),
	})
}

// AddContact adds a new contact for the current user.
func AddContact(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()

	var input struct {
		Phone string `json:"phone"`
		Type  string `json:"type"` // "casual" or "trusted"
	}
	if err := c.Bind(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if strings.TrimSpace(input.Phone) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "phone number is required"})
	}

	// Default to casual
	contactType := input.Type
	if contactType == "" {
		contactType = "casual"
	}
	if contactType != "casual" && contactType != "trusted" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "type must be 'casual' or 'trusted'"})
	}

	// Find user by phone number in profiles collection
	targetProfile, err := queryAppWriteDocumentByField(cfg, "profiles", "phone", strings.TrimSpace(input.Phone))
	if err != nil || targetProfile == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "no user found with this phone number"})
	}

	targetUserID, _ := targetProfile["userId"].(string)

	// Can't add yourself
	if targetUserID == userID {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "you cannot add yourself as a contact"})
	}

	// Check if already added
	existing, _ := queryAppWriteDocuments(cfg, "contacts", "ownerId", userID)
	for _, doc := range existing {
		if doc["contactUserId"] == targetUserID {
			return c.JSON(http.StatusConflict, map[string]string{"error": "contact already added"})
		}
	}

	// Enforce trusted limit (max 5)
	if contactType == "trusted" {
		trustedCount := 0
		for _, doc := range existing {
			if doc["type"] == "trusted" {
				trustedCount++
			}
		}
		if trustedCount >= 5 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "maximum 5 trusted contacts allowed"})
		}
	}

	// Create contact document
	data := map[string]interface{}{
		"ownerId":       userID,
		"contactUserId": targetUserID,
		"type":          contactType,
		"status":        "accepted", // For MVP, auto-accept
	}

	doc, err := createAppWriteDocument(cfg, "contacts", data)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to add contact"})
	}

	// Also add reverse contact so both can see each other
	reverseData := map[string]interface{}{
		"ownerId":       targetUserID,
		"contactUserId": userID,
		"type":          "casual", // Reverse is always casual by default
		"status":        "accepted",
	}
	createAppWriteDocument(cfg, "contacts", reverseData)

	return c.JSON(http.StatusCreated, doc)
}

// UpdateContact updates a contact's type (casual/trusted).
func UpdateContact(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetAppWriteConfig()
	contactID := c.Param("id")

	var input struct {
		Type string `json:"type"`
	}
	if err := c.Bind(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if input.Type != "casual" && input.Type != "trusted" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "type must be 'casual' or 'trusted'"})
	}

	// Enforce trusted limit when promoting to trusted
	if input.Type == "trusted" {
		existing, _ := queryAppWriteDocuments(cfg, "contacts", "ownerId", userID)
		trustedCount := 0
		for _, doc := range existing {
			if doc["type"] == "trusted" {
				trustedCount++
			}
		}
		if trustedCount >= 5 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "maximum 5 trusted contacts allowed"})
		}
	}

	doc, err := updateAppWriteDocument(cfg, "contacts", contactID, map[string]interface{}{
		"type": input.Type,
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update contact"})
	}

	return c.JSON(http.StatusOK, doc)
}

// DeleteContact removes a contact.
func DeleteContact(c echo.Context) error {
	cfg := config.GetAppWriteConfig()
	contactID := c.Param("id")

	err := deleteAppWriteDocument(cfg, "contacts", contactID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete contact"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "contact removed"})
}

// SearchUserByPhone searches for a user by phone number.
func SearchUserByPhone(c echo.Context) error {
	cfg := config.GetAppWriteConfig()
	phone := c.QueryParam("phone")

	if strings.TrimSpace(phone) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "phone query param is required"})
	}

	profile, err := queryAppWriteDocumentByField(cfg, "profiles", "phone", strings.TrimSpace(phone))
	if err != nil || profile == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "no user found"})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"userId": profile["userId"],
		"name":   profile["name"],
		"phone":  profile["phone"],
	})
}

// ---------- Additional AppWrite helpers ----------

func queryAppWriteDocuments(cfg *config.Config, collectionID, field, value string) ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/databases/%s/collections/%s/documents?queries[]=equal(\"%s\",[\"%s\"])&queries[]=limit(100)",
		cfg.AppWriteEndpoint, cfg.AppWriteDBID, collectionID, field, value)

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
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Documents, nil
}

func queryAppWriteDocumentByField(cfg *config.Config, collectionID, field, value string) (map[string]interface{}, error) {
	docs, err := queryAppWriteDocuments(cfg, collectionID, field, value)
	if err != nil || len(docs) == 0 {
		return nil, fmt.Errorf("not found")
	}
	return docs[0], nil
}

func deleteAppWriteDocument(cfg *config.Config, collectionID, docID string) error {
	url := fmt.Sprintf("%s/databases/%s/collections/%s/documents/%s",
		cfg.AppWriteEndpoint, cfg.AppWriteDBID, collectionID, docID)

	req, _ := http.NewRequest("DELETE", url, nil)
	req.Header.Set("X-Appwrite-Project", cfg.AppWriteProjectID)
	req.Header.Set("X-Appwrite-Key", cfg.AppWriteAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("AppWrite error (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}
