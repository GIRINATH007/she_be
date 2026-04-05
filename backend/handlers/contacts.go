package handlers

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
)

// GetContacts lists all contacts for the current user.
func GetContacts(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()

	docs, err := querySupabaseDocuments(cfg, "contacts", "ownerId", userID)
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
	cfg := config.GetSupabaseConfig()

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
	targetProfile, err := querySupabaseDocumentByField(cfg, "profiles", "phone", strings.TrimSpace(input.Phone))
	if err != nil || targetProfile == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "no user found with this phone number"})
	}

	targetUserID, _ := targetProfile["userId"].(string)

	// Can't add yourself
	if targetUserID == userID {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "you cannot add yourself as a contact"})
	}

	// Check if already added
	existing, _ := querySupabaseDocuments(cfg, "contacts", "ownerId", userID)
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

	doc, err := createSupabaseDocument(cfg, "contacts", data)
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
	createSupabaseDocument(cfg, "contacts", reverseData)

	return c.JSON(http.StatusCreated, doc)
}

// UpdateContact updates a contact's type (casual/trusted).
func UpdateContact(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()
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

	existingContact, err := getSupabaseDocument(cfg, "contacts", contactID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "contact not found"})
	}

	ownerID, _ := existingContact["ownerId"].(string)
	if ownerID != userID {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "you cannot update this contact"})
	}

	// Enforce trusted limit when promoting to trusted
	if input.Type == "trusted" {
		existing, _ := querySupabaseDocuments(cfg, "contacts", "ownerId", userID)
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

	doc, err := updateSupabaseDocument(cfg, "contacts", contactID, map[string]interface{}{
		"type": input.Type,
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update contact"})
	}

	return c.JSON(http.StatusOK, doc)
}

// DeleteContact removes a contact.
func DeleteContact(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()
	contactID := c.Param("id")

	existingContact, err := getSupabaseDocument(cfg, "contacts", contactID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "contact not found"})
	}

	ownerID, _ := existingContact["ownerId"].(string)
	if ownerID != userID {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "you cannot delete this contact"})
	}

	contacts, _ := querySupabaseDocuments(cfg, "contacts", "ownerId", userID)
	if len(contacts) <= 1 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "at least one contact is required"})
	}

	err = deleteSupabaseDocument(cfg, "contacts", contactID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete contact"})
	}

	contactUserID, _ := existingContact["contactUserId"].(string)
	reverseDocs, _ := querySupabaseDocuments(cfg, "contacts", "ownerId", contactUserID)
	for _, doc := range reverseDocs {
		reverseContactUserID, _ := doc["contactUserId"].(string)
		reverseDocID, _ := doc["$id"].(string)
		if reverseContactUserID == userID && reverseDocID != "" {
			_ = deleteSupabaseDocument(cfg, "contacts", reverseDocID)
			break
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "contact removed"})
}

// SearchUserByPhone searches for a user by phone number.
func SearchUserByPhone(c echo.Context) error {
	cfg := config.GetSupabaseConfig()
	phone := c.QueryParam("phone")

	if strings.TrimSpace(phone) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "phone query param is required"})
	}

	profile, err := querySupabaseDocumentByField(cfg, "profiles", "phone", strings.TrimSpace(phone))
	if err != nil || profile == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "no user found"})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"userId": profile["userId"],
		"name":   profile["name"],
		"phone":  profile["phone"],
	})
}
