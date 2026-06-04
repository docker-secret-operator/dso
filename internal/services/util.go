package services

import (
	"github.com/google/uuid"
)

// generateID generates a unique ID
func generateID() string {
	return uuid.New().String()
}
