package config

import (
	"log"
	"sync"
)

var (
	supabaseCfg *Config
	once        sync.Once
)

// InitSupabase initializes the global Supabase configuration.
func InitSupabase(cfg *Config) {
	once.Do(func() {
		if cfg.SupabaseURL == "" {
			log.Println("WARNING: SUPABASE_URL is not set")
		}
		supabaseCfg = cfg
		log.Printf("Supabase configured: url=%s", cfg.SupabaseURL)
	})
}

// GetSupabaseConfig returns the global Supabase configuration.
func GetSupabaseConfig() *Config {
	return supabaseCfg
}
