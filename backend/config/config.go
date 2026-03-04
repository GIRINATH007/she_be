package config

import "os"

// Config holds all configuration for the application.
type Config struct {
	AppWriteEndpoint  string
	AppWriteProjectID string
	AppWriteAPIKey    string
	AppWriteDBID      string
	AgoraAppID        string
	AgoraAppCert      string
	FCMServerKey      string
	Port              string
}

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		AppWriteEndpoint:  getEnv("APPWRITE_ENDPOINT", "http://localhost/v1"),
		AppWriteProjectID: getEnv("APPWRITE_PROJECT_ID", ""),
		AppWriteAPIKey:    getEnv("APPWRITE_API_KEY", ""),
		AppWriteDBID:      getEnv("APPWRITE_DB_ID", "sheguard"),
		AgoraAppID:        getEnv("AGORA_APP_ID", ""),
		AgoraAppCert:      getEnv("AGORA_APP_CERT", ""),
		FCMServerKey:      getEnv("FCM_SERVER_KEY", ""),
		Port:              getEnv("PORT", "8080"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
