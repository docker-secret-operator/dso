package services

import "fmt"

// Review Status Lifecycle:
// draft_review → active_review → (approved | rejected) → closed

// AllowedReviewTransitions defines valid review status transitions
var AllowedReviewTransitions = []ValidStatusTransition{
	// Draft review can move to active when review starts
	{From: "draft_review", To: "active_review"},
	// Active review can be approved
	{From: "active_review", To: "approved"},
	// Active review can be rejected
	{From: "active_review", To: "rejected"},
	// Approved review can be closed (final state)
	{From: "approved", To: "closed"},
	// Rejected review can be closed (final state)
	{From: "rejected", To: "closed"},
}

// GetReviewStatusDescription returns a human-readable description of a review status
func GetReviewStatusDescription(status string) string {
	descriptions := map[string]string{
		"draft_review":  "In preparation, not yet active",
		"active_review": "Under review, awaiting decisions",
		"approved":      "Review approved, awaiting execution",
		"rejected":      "Review rejected, no changes",
		"closed":        "Review complete and archived",
	}
	if desc, ok := descriptions[status]; ok {
		return desc
	}
	return "Unknown status"
}

// ValidateReviewStatusTransition validates a review status transition
func ValidateReviewStatusTransition(currentStatus, newStatus string) error {
	// Self-transitions are allowed (idempotent)
	if currentStatus == newStatus {
		return nil
	}

	if !IsValidTransition(currentStatus, newStatus) {
		allowed := GetAllowedReviewNextStatuses(currentStatus)
		return fmt.Errorf("invalid review status transition: %s → %s (allowed: %v)",
			currentStatus, newStatus, allowed)
	}

	return nil
}

// GetAllowedReviewNextStatuses returns all valid next statuses for a review
func GetAllowedReviewNextStatuses(current string) []string {
	var next []string
	for _, transition := range AllowedReviewTransitions {
		if transition.From == current {
			next = append(next, transition.To)
		}
	}
	return next
}

// IsTerminalReviewStatus checks if a review status is terminal (no more transitions)
func IsTerminalReviewStatus(status string) bool {
	return status == "closed"
}

// CanTransitionToActive checks if a review can transition to active
func CanTransitionToActive(status string) bool {
	return status == "draft_review"
}

// CanFinalize checks if a review can be finalized (moved to approved/rejected)
func CanFinalize(status string) bool {
	return status == "active_review"
}

// CanClose checks if a review can be closed
func CanClose(status string) bool {
	return status == "approved" || status == "rejected"
}
