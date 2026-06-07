package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	// BcryptCost is the cost parameter for bcrypt hashing
	BcryptCost = 12
	// MinPasswordLength is the minimum allowed password length
	MinPasswordLength = 8
)

// HashPassword hashes a plaintext password using bcrypt
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLength {
		return "", fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	return string(hash), nil
}

// VerifyPassword compares a plaintext password with a bcrypt hash
func VerifyPassword(password, hash string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err == bcrypt.ErrMismatchedHashAndPassword {
		return fmt.Errorf("password mismatch")
	}
	if err != nil {
		return fmt.Errorf("password verification failed: %w", err)
	}
	return nil
}
