package drift

import (
	"time"
)

// DriftType represents the type of drift detected
type DriftType string

const (
	DriftSecret        DriftType = "secret"
	DriftPolicy        DriftType = "policy"
	DriftPlugin        DriftType = "plugin"
	DriftUser          DriftType = "user"
	DriftConfiguration DriftType = "configuration"
	DriftBackup        DriftType = "backup"
	DriftIntegration   DriftType = "integration"
	DriftScheduler     DriftType = "scheduler"
	// Real secret-version drift types (P4)
	DriftVersionMismatch DriftType = "version_mismatch"
	DriftStaleSecret     DriftType = "stale_secret"
	DriftMissingSecret   DriftType = "missing_secret"
	DriftRotationLag     DriftType = "rotation_lag"
)

// DriftSeverity represents the severity of drift
type DriftSeverity string

const (
	SeverityInfo     DriftSeverity = "info"
	SeverityLow      DriftSeverity = "low"
	SeverityMedium   DriftSeverity = "medium"
	SeverityHigh     DriftSeverity = "high"
	SeverityCritical DriftSeverity = "critical"
)

// DriftStatus represents the status of drift
type DriftStatus string

const (
	StatusDetected      DriftStatus = "detected"
	StatusAcknowledged  DriftStatus = "acknowledged"
	StatusResolved      DriftStatus = "resolved"
)

// DriftFinding represents a detected drift
type DriftFinding struct {
	ID              string
	Type            DriftType
	Severity        DriftSeverity
	Status          DriftStatus
	Resource        string
	Description     string
	Metadata        map[string]interface{}
	DetectedAt      time.Time
	AcknowledgedAt  *time.Time
	ResolvedAt      *time.Time
}

// DriftScan represents a scan execution
type DriftScan struct {
	ID          string
	DetectorID  string
	FindingsCount int
	Duration    time.Duration
	Success     bool
	Error       string
	CreatedAt   time.Time
}

// DriftMetrics tracks drift metrics
type DriftMetrics struct {
	TotalFindings    int
	CriticalFindings int
	OpenFindings     int
	Scans            int
	AverageDuration  float64
	LastScan         *time.Time
	FindingsByType   map[DriftType]int
	FindingsBySeverity map[DriftSeverity]int
}

// Detector defines the interface for drift detectors
type Detector interface {
	ID() string
	Name() string
	Type() DriftType
	Detect(context interface{}) ([]DriftFinding, error)
}
