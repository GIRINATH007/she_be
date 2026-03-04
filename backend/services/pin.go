package services

import (
	"golang.org/x/crypto/bcrypt"
)

// HashPIN hashes a 4-digit PIN using bcrypt.
func HashPIN(pin string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPIN checks a plain PIN against a bcrypt hash.
func VerifyPIN(pin, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pin))
	return err == nil
}
