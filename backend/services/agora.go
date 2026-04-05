package services

import (
	"fmt"
	"time"

	rtctokenbuilder "github.com/AgoraIO-Community/go-tokenbuilder/rtctokenbuilder"
	"github.com/sheguard/backend/config"
)

// GenerateAgoraToken generates an RTC token for a given channel and user.
// The token expires after the specified duration.
func GenerateAgoraToken(channelName string, uid uint32, expireSeconds uint32) (string, error) {
	cfg := config.GetSupabaseConfig()

	if cfg.AgoraAppID == "" || cfg.AgoraAppCert == "" {
		return "", fmt.Errorf("Agora credentials not configured")
	}

	expireTimestamp := uint32(time.Now().UTC().Unix()) + expireSeconds

	token, err := rtctokenbuilder.BuildTokenWithUid(
		cfg.AgoraAppID,
		cfg.AgoraAppCert,
		channelName,
		uid,
		rtctokenbuilder.RolePublisher,
		expireTimestamp,
	)
	if err != nil {
		return "", fmt.Errorf("failed to generate Agora token: %w", err)
	}

	return token, nil
}

// MaxSOSDurationSeconds is the maximum SOS session duration (10 minutes)
// to protect Agora free tier usage.
const MaxSOSDurationSeconds uint32 = 600
