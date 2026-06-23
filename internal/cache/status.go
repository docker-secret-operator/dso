package cache

import (
	"sync"
	"time"
)

// EvalStatus records the last evaluation timestamps and durations for the four
// P10-tracked operations. It is exposed via the /api/status endpoint so
// operators can see whether the system is producing fresh results.
// Stale state is never hidden.
type EvalStatus struct {
	mu sync.RWMutex

	LastRecommendationEval time.Time
	LastForecastEval       time.Time
	LastDriftScan          time.Time
	LastComplianceEval     time.Time

	RecommendationEvalDur time.Duration
	ForecastEvalDur       time.Duration
	DriftScanDur          time.Duration
	ComplianceEvalDur     time.Duration

	RecommendationCount int
	ForecastCount       int
}

// RecordRecommendation records a recommendation evaluation.
func (s *EvalStatus) RecordRecommendation(dur time.Duration, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastRecommendationEval = time.Now().UTC()
	s.RecommendationEvalDur = dur
	s.RecommendationCount = count
}

// RecordForecast records a forecast evaluation.
func (s *EvalStatus) RecordForecast(dur time.Duration, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastForecastEval = time.Now().UTC()
	s.ForecastEvalDur = dur
	s.ForecastCount = count
}

// RecordDriftScan records a drift scan.
func (s *EvalStatus) RecordDriftScan(dur time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastDriftScan = time.Now().UTC()
	s.DriftScanDur = dur
}

// RecordCompliance records a compliance evaluation.
func (s *EvalStatus) RecordCompliance(dur time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastComplianceEval = time.Now().UTC()
	s.ComplianceEvalDur = dur
}

// Snapshot returns a point-in-time copy of all fields — safe to read without holding the lock.
func (s *EvalStatus) Snapshot() EvalStatusSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return EvalStatusSnapshot{
		LastRecommendationEval: s.LastRecommendationEval,
		LastForecastEval:       s.LastForecastEval,
		LastDriftScan:          s.LastDriftScan,
		LastComplianceEval:     s.LastComplianceEval,
		RecommendationEvalMs:   s.RecommendationEvalDur.Milliseconds(),
		ForecastEvalMs:         s.ForecastEvalDur.Milliseconds(),
		DriftScanMs:            s.DriftScanDur.Milliseconds(),
		ComplianceEvalMs:       s.ComplianceEvalDur.Milliseconds(),
		RecommendationCount:    s.RecommendationCount,
		ForecastCount:          s.ForecastCount,
	}
}

// EvalStatusSnapshot is a lock-free copy for serialisation.
type EvalStatusSnapshot struct {
	LastRecommendationEval time.Time `json:"last_recommendation_eval"`
	LastForecastEval       time.Time `json:"last_forecast_eval"`
	LastDriftScan          time.Time `json:"last_drift_scan"`
	LastComplianceEval     time.Time `json:"last_compliance_eval"`

	RecommendationEvalMs int64 `json:"recommendation_eval_ms"`
	ForecastEvalMs       int64 `json:"forecast_eval_ms"`
	DriftScanMs          int64 `json:"drift_scan_ms"`
	ComplianceEvalMs     int64 `json:"compliance_eval_ms"`

	RecommendationCount int `json:"recommendation_count"`
	ForecastCount       int `json:"forecast_count"`
}
