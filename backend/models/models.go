package models

import "time"

// Profile represents a user's profile information.
type Profile struct {
	ID          string `json:"$id,omitempty"`
	UserID      string `json:"userId"`
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	BloodGroup  string `json:"bloodGroup,omitempty"`
	Allergies   string `json:"allergies,omitempty"`
	Medications string `json:"medications,omitempty"`
	PinHash     string `json:"pinHash,omitempty"`
	FCMToken    string `json:"fcmToken,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
}

// Contact represents a relationship between two users.
type Contact struct {
	ID            string `json:"$id,omitempty"`
	OwnerID       string `json:"ownerId"`
	ContactUserID string `json:"contactUserId"`
	Type          string `json:"type"`   // "casual" or "trusted"
	Status        string `json:"status"` // "pending", "accepted", "rejected"
}

// Location represents a user's current position.
type Location struct {
	UserID    string  `json:"userId"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Accuracy  float64 `json:"accuracy"`
	Timestamp int64   `json:"timestamp"`
}

// SOSEvent represents an active SOS alert.
type SOSEvent struct {
	ID           string    `json:"$id,omitempty"`
	TriggeredBy  string    `json:"triggeredBy"`
	Type         string    `json:"type"`   // "timer" or "instant"
	Status       string    `json:"status"` // "active" or "resolved"
	AgoraChannel string    `json:"agoraChannel"`
	StartedAt    time.Time `json:"startedAt"`
	EndedAt      time.Time `json:"endedAt,omitempty"`
}

// WalkSession represents a Walk With Me session.
type WalkSession struct {
	ID          string    `json:"$id,omitempty"`
	RequesterID string    `json:"requesterId"`
	AccepterID  string    `json:"accepterId,omitempty"`
	InvitedIDs  []string  `json:"invitedIds"`
	Status      string    `json:"status"` // "pending", "active", "completed", "cancelled"
	StartedAt   time.Time `json:"startedAt"`
	EndedAt     time.Time `json:"endedAt,omitempty"`
}
