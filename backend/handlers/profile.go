package handlers

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
	sgmiddleware "github.com/sheguard/backend/middleware"
	"github.com/sheguard/backend/models"
	"github.com/sheguard/backend/services"
)

// GetProfile returns the current user's profile from Supabase.
func GetProfile(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()

	doc, err := queryProfileByUserID(cfg, userID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "profile not found",
		})
	}

	return c.JSON(http.StatusOK, doc)
}

// UpdateProfile creates or updates the current user's profile in Supabase.
func UpdateProfile(c echo.Context) error {
	userID := c.Get("userId").(string)
	cfg := config.GetSupabaseConfig()

	var input struct {
		Name        *string `json:"name"`
		Phone       *string `json:"phone"`
		BloodGroup  *string `json:"bloodGroup"`
		Allergies   *string `json:"allergies"`
		Medications *string `json:"medications"`
		PinHash     *string `json:"pinHash"`
		FCMToken    *string `json:"fcmToken"`
	}

	if err := c.Bind(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	existingDoc, _ := queryProfileByUserID(cfg, userID)
	isCreate := existingDoc == nil

	data := map[string]interface{}{}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "name cannot be empty"})
		}
		data["name"] = sgmiddleware.SanitizeString(name)
	}
	if input.Phone != nil {
		phone := strings.TrimSpace(*input.Phone)
		if phone == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "phone cannot be empty"})
		}
		data["phone"] = sgmiddleware.SanitizeString(phone)
	}
	if input.BloodGroup != nil {
		data["bloodGroup"] = sgmiddleware.SanitizeString(*input.BloodGroup)
	}
	if input.Allergies != nil {
		data["allergies"] = sgmiddleware.SanitizeString(*input.Allergies)
	}
	if input.Medications != nil {
		data["medications"] = sgmiddleware.SanitizeString(*input.Medications)
	}
	if input.PinHash != nil {
		pin := strings.TrimSpace(*input.PinHash)
		if pin == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "pinHash cannot be empty"})
		}
		hashed, err := services.HashPIN(pin)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to process pin"})
		}
		data["pinHash"] = hashed
	}
	if input.FCMToken != nil {
		data["fcmToken"] = strings.TrimSpace(*input.FCMToken)
	}

	if isCreate {
		name, _ := data["name"].(string)
		phone, _ := data["phone"].(string)
		if strings.TrimSpace(name) == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
		}
		if strings.TrimSpace(phone) == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "phone is required"})
		}
		data["userId"] = userID

		result, err := createSupabaseDocument(cfg, "profiles", data)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create profile: " + err.Error()})
		}
		return c.JSON(http.StatusCreated, result)
	}

	if len(data) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no fields provided for update"})
	}

	docID, _ := existingDoc["$id"].(string)
	result, err := updateSupabaseDocument(cfg, "profiles", docID, data)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update profile"})
	}
	return c.JSON(http.StatusOK, result)
}

// Ensure models import is used (for future type safety)
var _ = models.Profile{}
