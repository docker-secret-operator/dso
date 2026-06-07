package services

import "fmt"

// Approval Status Lifecycle:
// pending → approved → closed
// pending → rejected → closed
// pending → expired → closed (no action taken within timeout)

// AllowedApprovalTransitions defines valid approval status transitions
var AllowedApprovalTransitions = []ValidStatusTransition{
	// Pending can transition to approved
	{From: "pending", To: "approved"},
	// Pending can transition to rejected
	{From: "pending", To: "rejected"},
	// Pending can expire
	{From: "pending", To: "expired"},
	// Approved can transition to closed
	{From: "approved", To: "closed"},
	// Rejected can transition to closed
	{From: "rejected", To: "closed"},
	// Expired can transition to closed
	{From: "expired", To: "closed"},
}

// GetApprovalStatusDescription returns a human-readable description of an approval status
func GetApprovalStatusDescription(status string) string {
	descriptions := map[string]string{
		"pending":   "Awaiting reviewer decision",
		"approved":  "Approved by reviewer",
		"rejected":  "Rejected by reviewer",
		"expired":   "No decision within timeout period",
		"closed":    "Approval process complete",
	}
	if desc, ok := descriptions[status]; ok {
		return desc
	}
	return "Unknown status"
}

// ValidateApprovalStatusTransition validates an approval status transition
func ValidateApprovalStatusTransition(currentStatus, newStatus string) error {
	// Self-transitions are allowed (idempotent)
	if currentStatus == newStatus {
		return nil
	}

	if !IsValidApprovalTransition(currentStatus, newStatus) {
		allowed := GetAllowedApprovalNextStatuses(currentStatus)
		return fmt.Errorf("invalid approval status transition: %s → %s (allowed: %v)",
			currentStatus, newStatus, allowed)
	}

	return nil
}

// IsValidApprovalTransition checks if a transition is valid
func IsValidApprovalTransition(from, to string) bool {
	for _, transition := range AllowedApprovalTransitions {
		if transition.From == from && transition.To == to {
			return true
		}
	}
	return false
}

// GetAllowedApprovalNextStatuses returns all valid next statuses for an approval
func GetAllowedApprovalNextStatuses(current string) []string {
	var next []string
	for _, transition := range AllowedApprovalTransitions {
		if transition.From == current {
			next = append(next, transition.To)
		}
	}
	return next
}

// IsTerminalApprovalStatus checks if an approval status is terminal
func IsTerminalApprovalStatus(status string) bool {
	return status == "closed"
}

// CanApprove checks if an approval can be approved
func CanApprove(status string) bool {
	return status == "pending"
}

// CanReject checks if an approval can be rejected
func CanReject(status string) bool {
	return status == "pending"
}

// CanExpire checks if an approval can expire
func CanExpire(status string) bool {
	return status == "pending"
}

// CanCloseApproval checks if an approval can be closed
func CanCloseApproval(status string) bool {
	return status == "approved" || status == "rejected" || status == "expired"
}
