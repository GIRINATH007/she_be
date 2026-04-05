package config

import "os"

// Config holds all configuration for the application.
type Config struct {
	SupabaseURL            string
	SupabaseAnonKey        string
	SupabaseServiceRoleKey string
	AgoraAppID             string
	AgoraAppCert           string
	FCMServerKey           string
	GoogleApplicationCreds string
	FirebaseProjectID      string
	Port                   string
}

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		SupabaseURL:            getEnv("SUPABASE_URL", ""),
		SupabaseAnonKey:        getEnv("SUPABASE_ANON_KEY", ""),
		SupabaseServiceRoleKey: getEnv("SUPABASE_SERVICE_ROLE_KEY", ""),
		AgoraAppID:             getEnv("AGORA_APP_ID", ""),
		AgoraAppCert:           getEnv("AGORA_APP_CERT", ""),
		FCMServerKey:           getEnv("FCM_SERVER_KEY", ""),
		GoogleApplicationCreds: getEnv("GOOGLE_APPLICATION_CREDENTIALS", ""),
		FirebaseProjectID:      getEnv("FIREBASE_PROJECT_ID", ""),
		Port:                   getEnv("PORT", "8081"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
