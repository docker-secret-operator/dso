package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const (
	// TokenLength is the length of the random token in bytes (produces ~88 char base64)
	TokenLength = 64
)

// GenerateToken creates a cryptographically secure random token
func GenerateToken() (string, error) {
	b := make([]byte, TokenLength)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashToken creates a SHA256 hash of a token for database storage
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// VerifyToken compares a token with its hash
func VerifyToken(token, hash string) bool {
	return hash == HashToken(token)
}
