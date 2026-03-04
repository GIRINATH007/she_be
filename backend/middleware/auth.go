package middleware

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
)

// AuthMiddleware verifies the user by checking the X-User-ID header
// and validating the user exists in AppWrite via the server API key.
func AuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID := c.Request().Header.Get("X-User-ID")
		if userID == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"error": "missing X-User-ID header",
			})
		}

		// Verify user exists in AppWrite using server API key
		if err := verifyAppWriteUser(userID); err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"error": "invalid user: " + err.Error(),
			})
		}

		c.Set("userId", userID)
		return next(c)
	}
}

// verifyAppWriteUser calls AppWrite's Server Users API to verify user exists.
func verifyAppWriteUser(userID string) error {
	cfg := config.GetAppWriteConfig()
	url := fmt.Sprintf("%s/users/%s", cfg.AppWriteEndpoint, userID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("X-Appwrite-Project", cfg.AppWriteProjectID)
	req.Header.Set("X-Appwrite-Key", cfg.AppWriteAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach AppWrite: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("user not found (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID string `json:"$id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if result.ID != userID {
		return fmt.Errorf("user ID mismatch")
	}

	return nil
}
