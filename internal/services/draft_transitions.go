package services

import "fmt"

// ValidStatusTransition represents a valid status transition
type ValidStatusTransition struct {
	From string
	To   string
}

// AllowedTransitions defines all valid draft status transitions
var AllowedTransitions = []ValidStatusTransition{
	// draft → under_review
	{From: "draft", To: "under_review"},
	// under_review → approved
	{From: "under_review", To: "approved"},
	// under_review → rejected
	{From: "under_review", To: "rejected"},
	// approved → archived
	{From: "approved", To: "archived"},
	// rejected → archived
	{From: "rejected", To: "archived"},
}

// IsValidTransition checks if a status transition is allowed
func IsValidTransition(from, to string) bool {
	for _, transition := range AllowedTransitions {
		if transition.From == from && transition.To == to {
			return true
		}
	}
	return false
}

// GetAllowedNextStatuses returns all valid next statuses for a given current status
func GetAllowedNextStatuses(current string) []string {
	var next []string
	for _, transition := range AllowedTransitions {
		if transition.From == current {
			next = append(next, transition.To)
		}
	}
	return next
}

// ValidateDraftStatusTransition validates a status transition
func ValidateDraftStatusTransition(currentStatus, newStatus string) error {
	// Self-transitions are allowed (idempotent)
	if currentStatus == newStatus {
		return nil
	}

	if !IsValidTransition(currentStatus, newStatus) {
		allowed := GetAllowedNextStatuses(currentStatus)
		return fmt.Errorf("invalid status transition: %s → %s (allowed: %v)", currentStatus, newStatus, allowed)
	}

	return nil
}
