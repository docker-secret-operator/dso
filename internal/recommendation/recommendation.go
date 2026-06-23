package recommendation

import (
	"time"
)

// Priority represents recommendation priority
type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityMedium   Priority = "medium"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

// Category represents recommendation category
type Category string

const (
	CategoryBackup      Category = "backup"
	CategorySecurity    Category = "security"
	CategoryPlugin      Category = "plugin"
	CategoryIntegration Category = "integration"
	CategoryScheduler   Category = "scheduler"
	CategoryPolicy      Category = "policy"
	CategoryDrift       Category = "drift"
	CategoryPerformance Category = "performance"
	// P8 evidence-derived categories
	CategoryRotation    Category = "rotation"
	CategoryCompliance  Category = "compliance"
	CategoryOperational Category = "operational"
)

// Status represents recommendation status
type Status string

const (
	StatusOpen        Status = "open"
	StatusAcknowledged Status = "acknowledged"
	StatusImplemented Status = "implemented"
	StatusDismissed   Status = "dismissed"
)

// Recommendation represents an operational recommendation
type Recommendation struct {
	ID              string
	Title           string
	Description     string
	Reason          string   // why this recommendation exists (evidence statement)
	Resource        string   // secret/policy/rule name this applies to
	Priority        Priority
	Category        Category
	Status          Status
	ResourceID      string
	IncidentID      string
	SuggestedAction string
	Confidence      float64
	// Cross-link fields (P8)
	DriftID  string
	PolicyID string
	AuditID  string
	CreatedAt      time.Time
	AcknowledgedAt *time.Time
	ImplementedAt  *time.Time
	DismissedAt    *time.Time
	Metadata       map[string]string
}

// RecommendationMetrics tracks recommendation metrics
type RecommendationMetrics struct {
	TotalRecommendations      int
	OpenRecommendations       int
	AcknowledgedRecommendations int
	ImplementedRecommendations int
	DismissedRecommendations  int
	AverageConfidence         float64
	LastUpdate                time.Time
}

// RecommendationRule defines a rule for generating recommendations
type RecommendationRule struct {
	ID          string
	Name        string
	Description string
	Enabled     bool
	Priority    int
	Category    Category
	TriggerType string // incident, drift, alert, plugin_failure, scheduler_failure, integration_failure, policy_failure
	Actions     []string
	Conditions  map[string]interface{}
}

// PriorityLevel returns numeric score for priority
func (p Priority) Score() int {
	switch p {
	case PriorityCritical:
		return 4
	case PriorityHigh:
		return 3
	case PriorityMedium:
		return 2
	case PriorityLow:
		return 1
	default:
		return 0
	}
}

// PriorityFromScore converts score to priority
func PriorityFromScore(score int) Priority {
	switch score {
	case 4:
		return PriorityCritical
	case 3:
		return PriorityHigh
	case 2:
		return PriorityMedium
	case 1:
		return PriorityLow
	default:
		return PriorityLow
	}
}

// IsResolved returns true if recommendation is resolved
func (r *Recommendation) IsResolved() bool {
	return r.Status == StatusImplemented || r.Status == StatusDismissed
}

// Duration returns the duration since creation
func (r *Recommendation) Duration() time.Duration {
	end := time.Now()
	if r.ImplementedAt != nil {
		end = *r.ImplementedAt
	} else if r.DismissedAt != nil {
		end = *r.DismissedAt
	}
	return end.Sub(r.CreatedAt)
}
