package config

import (
	"log"
	"sync"
)

// AppWriteConfig holds the AppWrite connection details accessible globally.
var (
	appWriteCfg *Config
	once        sync.Once
)

// InitAppWrite initializes the global AppWrite configuration.
func InitAppWrite(cfg *Config) {
	once.Do(func() {
		if cfg.AppWriteProjectID == "" {
			log.Println("WARNING: APPWRITE_PROJECT_ID is not set")
		}
		appWriteCfg = cfg
		log.Printf("AppWrite configured: endpoint=%s project=%s db=%s",
			cfg.AppWriteEndpoint, cfg.AppWriteProjectID, cfg.AppWriteDBID)
	})
}

// GetAppWriteConfig returns the global AppWrite configuration.
func GetAppWriteConfig() *Config {
	return appWriteCfg
}
