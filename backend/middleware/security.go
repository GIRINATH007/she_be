package middleware

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
)

// inputSanitization patterns
var (
	phoneRegex = regexp.MustCompile(`^[\d\s\+\-\(\)]{7,20}$`)
	emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	// Dangerous patterns for NoSQL injection prevention
	dangerousPatterns = []string{"$gt", "$lt", "$ne", "$regex", "$where", "$or", "$and"}
)

// SanitizeMiddleware validates and sanitizes request inputs.
func SanitizeMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Check for overly large request bodies (max 1MB)
		if c.Request().ContentLength > 1024*1024 {
			return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{
				"error": "request body too large (max 1MB)",
			})
		}
		return next(c)
	}
}

// SanitizeString removes potentially dangerous characters from input.
func SanitizeString(input string) string {
	s := strings.TrimSpace(input)
	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")
	// Check for NoSQL injection patterns
	for _, pattern := range dangerousPatterns {
		s = strings.ReplaceAll(s, pattern, "")
	}
	return s
}

// ValidatePhone checks if a phone number is in valid format.
func ValidatePhone(phone string) bool {
	return phoneRegex.MatchString(strings.TrimSpace(phone))
}

// ValidateEmail checks if an email is in valid format.
func ValidateEmail(email string) bool {
	return emailRegex.MatchString(strings.TrimSpace(email))
}

// SecurityHeaders adds security headers to responses.
func SecurityHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set("X-Content-Type-Options", "nosniff")
		c.Response().Header().Set("X-Frame-Options", "DENY")
		c.Response().Header().Set("X-XSS-Protection", "1; mode=block")
		c.Response().Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		return next(c)
	}
}
