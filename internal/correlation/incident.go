package correlation

import (
	"time"
)

// Severity represents incident severity level
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// IncidentStatus represents incident status
type IncidentStatus string

const (
	StatusOpen         IncidentStatus = "open"
	StatusAcknowledged IncidentStatus = "acknowledged"
	StatusResolved     IncidentStatus = "resolved"
)

// Incident represents a correlated incident
type Incident struct {
	ID               string
	Title            string
	Severity         Severity
	Status           IncidentStatus
	RootCause        string
	AffectedNodes    []string
	RelatedEvents    []string
	EventCount       int
	FirstSeen        time.Time
	LastSeen         time.Time
	CorrelationScore float64
	AcknowledgedAt   *time.Time
	ResolvedAt       *time.Time
	Metadata         map[string]string
}

// IncidentEvent represents an event associated with an incident
type IncidentEvent struct {
	ID              string
	IncidentID      string
	EventID         string
	EventType       string
	EventData       map[string]interface{}
	CorrelationKey  string
	CreatedAt       time.Time
}

// CorrelationRule represents a rule for grouping events
type CorrelationRule struct {
	ID          string
	Name        string
	Description string
	Enabled     bool
	Priority    int
	GroupBy     []string        // Fields to group by (node_id, event_type, etc.)
	TimeWindow  time.Duration   // Time window for grouping
	MinEvents   int             // Minimum events to create incident
	Filters     map[string]string // Event filters
}

// CorrelationKey represents a unique correlation key
type CorrelationKey struct {
	Rule   string
	Values map[string]string
}

// IncidentMetrics tracks incident metrics
type IncidentMetrics struct {
	TotalIncidents      int
	OpenIncidents       int
	ResolvedIncidents   int
	AcknowledgedIncidents int
	AverageScore        float64
	EventsProcessed     int
	MergesPerformed     int
	LastUpdate          time.Time
}

// EventBusEvent represents an event from the event bus
type EventBusEvent struct {
	Type          string
	Timestamp     time.Time
	CorrelationID string
	Payload       map[string]interface{}
	Source        string
}

// SeverityLevel converts string to severity level
func SeverityLevel(s string) Severity {
	switch s {
	case "critical":
		return SeverityCritical
	case "high":
		return SeverityHigh
	case "medium":
		return SeverityMedium
	case "low":
		return SeverityLow
	default:
		return SeverityInfo
	}
}

// SeverityScore returns numeric score for severity
func (s Severity) Score() int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	default:
		return 1
	}
}

// IsOpen returns true if incident is open
func (i *Incident) IsOpen() bool {
	return i.Status == StatusOpen
}

// IsResolved returns true if incident is resolved
func (i *Incident) IsResolved() bool {
	return i.Status == StatusResolved
}

// Duration returns the duration of the incident
func (i *Incident) Duration() time.Duration {
	end := i.LastSeen
	if i.ResolvedAt != nil {
		end = *i.ResolvedAt
	}
	return end.Sub(i.FirstSeen)
}
