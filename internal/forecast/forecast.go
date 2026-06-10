package forecast

import (
	"time"
)

// ForecastResource represents the type of resource being forecasted
type ForecastResource string

const (
	ResourceQueue           ForecastResource = "queue"
	ResourceMemory          ForecastResource = "memory"
	ResourceBackupStorage   ForecastResource = "backup_storage"
	ResourceIncident        ForecastResource = "incident"
	ResourceExecution       ForecastResource = "execution"
	ResourceAlert           ForecastResource = "alert"
	ResourcePlugin          ForecastResource = "plugin"
	ResourceIntegration     ForecastResource = "integration"
	ResourceScheduler       ForecastResource = "scheduler"
)

// ForecastHorizon represents the prediction time horizon
type ForecastHorizon string

const (
	HorizonOneHour   ForecastHorizon = "1h"
	HorizonSixHours  ForecastHorizon = "6h"
	HorizonOneDay    ForecastHorizon = "1d"
	HorizonSevenDays ForecastHorizon = "7d"
	HorizonThirtyDays ForecastHorizon = "30d"
)

// ForecastSeverity represents forecast severity
type ForecastSeverity string

const (
	SeverityInfo     ForecastSeverity = "info"
	SeverityLow      ForecastSeverity = "low"
	SeverityMedium   ForecastSeverity = "medium"
	SeverityHigh     ForecastSeverity = "high"
	SeverityCritical ForecastSeverity = "critical"
)

// Trend represents the trend direction
type Trend string

const (
	TrendIncreasing Trend = "increasing"
	TrendStable     Trend = "stable"
	TrendDecreasing Trend = "decreasing"
)

// Forecast represents a prediction for future operational conditions
type Forecast struct {
	ID             string
	ResourceType   ForecastResource
	ResourceID     string
	Metric         string
	CurrentValue   float64
	PredictedValue float64
	GrowthRate     float64
	Confidence     float64
	Horizon        ForecastHorizon
	Severity       ForecastSeverity
	Trend          Trend
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Metadata       map[string]string
}

// ForecastMetrics tracks forecasting metrics
type ForecastMetrics struct {
	TotalForecasts     int
	CriticalForecasts  int
	AverageConfidence  float64
	PredictionAccuracy float64
	ForecastRuns       int
	LastUpdate         time.Time
}

// DataPoint represents a historical data point for forecasting
type DataPoint struct {
	Timestamp time.Time
	Value     float64
}

// HorizonDuration returns the duration for a horizon
func (h ForecastHorizon) Duration() time.Duration {
	switch h {
	case HorizonOneHour:
		return 1 * time.Hour
	case HorizonSixHours:
		return 6 * time.Hour
	case HorizonOneDay:
		return 24 * time.Hour
	case HorizonSevenDays:
		return 7 * 24 * time.Hour
	case HorizonThirtyDays:
		return 30 * 24 * time.Hour
	default:
		return 1 * time.Hour
	}
}

// SeverityScore returns numeric score for severity
func (s ForecastSeverity) Score() int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// IsWorsening returns true if trend is worsening
func (f *Forecast) IsWorsening() bool {
	return f.Trend == TrendIncreasing && f.GrowthRate > 0.1
}

// IsRisky returns true if forecast indicates risk
func (f *Forecast) IsRisky() bool {
	return f.Severity == SeverityHigh || f.Severity == SeverityCritical
}

// DaysSinceCreation returns days since forecast was created
func (f *Forecast) DaysSinceCreation() float64 {
	return time.Since(f.CreatedAt).Hours() / 24
}
