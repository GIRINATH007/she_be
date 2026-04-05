package middleware

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sheguard/backend/config"
)

type supabaseUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// AuthMiddleware verifies Supabase access tokens and attaches user metadata.
func AuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		authHeader := c.Request().Header.Get(echo.HeaderAuthorization)
		if authHeader == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"error": "missing Authorization header",
			})
		}

		token, err := ExtractBearerToken(authHeader)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"error": err.Error(),
			})
		}

		user, err := VerifySupabaseAccessToken(token)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"error": "invalid token: " + err.Error(),
			})
		}

		c.Set("userId", user.ID)
		c.Set("userEmail", user.Email)
		return next(c)
	}
}

func ExtractBearerToken(authHeader string) (string, error) {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("invalid Authorization header format")
	}
	return strings.TrimSpace(parts[1]), nil
}

// VerifySupabaseAccessToken validates a Supabase JWT by calling Supabase Auth API.
func VerifySupabaseAccessToken(token string) (*supabaseUser, error) {
	cfg := config.GetSupabaseConfig()
	if cfg == nil {
		return nil, fmt.Errorf("server config not initialized")
	}
	if cfg.SupabaseURL == "" || cfg.SupabaseAnonKey == "" {
		return nil, fmt.Errorf("supabase auth config missing")
	}

	url := strings.TrimRight(cfg.SupabaseURL, "/") + "/auth/v1/user"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("apikey", cfg.SupabaseAnonKey)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach Supabase: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("auth failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result supabaseUser
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode auth response: %w", err)
	}
	if result.ID == "" {
		return nil, fmt.Errorf("missing user id in auth response")
	}

	return &result, nil
}
