package api

import (
	"math"
	"testing"
	"time"
)

// ---------- linearSlope ----------

func TestLinearSlope_Rising(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5}
	s := linearSlope(vals)
	if s < 0.9 || s > 1.1 {
		t.Errorf("expected slope ≈ 1, got %f", s)
	}
}

func TestLinearSlope_Flat(t *testing.T) {
	vals := []float64{3, 3, 3, 3, 3}
	s := linearSlope(vals)
	if math.Abs(s) > 1e-9 {
		t.Errorf("expected slope = 0, got %f", s)
	}
}

func TestLinearSlope_Falling(t *testing.T) {
	vals := []float64{5, 4, 3, 2, 1}
	s := linearSlope(vals)
	if s > -0.9 {
		t.Errorf("expected negative slope, got %f", s)
	}
}

func TestLinearSlope_Short(t *testing.T) {
	if linearSlope(nil) != 0 {
		t.Error("nil input should return 0")
	}
	if linearSlope([]float64{7}) != 0 {
		t.Error("single-element should return 0")
	}
}

// ---------- aggregateByGranularity ----------

func TestAggregateByGranularity_Empty(t *testing.T) {
	result := aggregateByGranularity(nil, time.Minute)
	if len(result) != 0 {
		t.Errorf("expected 0 points, got %d", len(result))
	}
}

func TestAggregateByGranularity_Buckets(t *testing.T) {
	base := int64(1700000000)
	pts := []*MetricsPoint{
		{Timestamp: base, SuccessRate: 0.9},
		{Timestamp: base + 30, SuccessRate: 0.8}, // same 1-min bucket
		{Timestamp: base + 90, SuccessRate: 0.7}, // next bucket
	}
	result := aggregateByGranularity(pts, time.Minute)
	if len(result) != 2 {
		t.Errorf("expected 2 buckets, got %d", len(result))
	}
	// First bucket avg of 0.9 and 0.8 = 0.85
	if math.Abs(result[0].SuccessRate-0.85) > 1e-9 {
		t.Errorf("expected bucket avg 0.85, got %f", result[0].SuccessRate)
	}
}

// ---------- computeTrends ----------

func TestComputeTrends_ImprovingSuccessRate(t *testing.T) {
	pts := make([]*MetricsPoint, 10)
	for i := range pts {
		pts[i] = &MetricsPoint{Timestamp: int64(i * 60), SuccessRate: float64(i) * 0.01}
	}
	tr := computeTrends(pts)
	if tr.SuccessRate.Direction != "improving" {
		t.Errorf("expected improving, got %s", tr.SuccessRate.Direction)
	}
	if tr.SuccessRate.Arrow != "↑" {
		t.Errorf("expected ↑, got %s", tr.SuccessRate.Arrow)
	}
}

func TestComputeTrends_StableSuccessRate(t *testing.T) {
	pts := make([]*MetricsPoint, 5)
	for i := range pts {
		pts[i] = &MetricsPoint{Timestamp: int64(i * 60), SuccessRate: 0.95}
	}
	tr := computeTrends(pts)
	if tr.SuccessRate.Direction != "stable" {
		t.Errorf("expected stable, got %s", tr.SuccessRate.Direction)
	}
}

// ---------- computeForecast ----------

func TestComputeForecast_NoData(t *testing.T) {
	fc := computeForecast(nil)
	if fc.QueueStatus != "healthy" {
		t.Errorf("expected healthy with no data")
	}
}

func TestComputeForecast_CriticalQueue(t *testing.T) {
	pts := make([]*MetricsPoint, 5)
	for i := range pts {
		// Queue grows from 30 to 70
		pts[i] = &MetricsPoint{Timestamp: int64(i * 3600), QueueDepth: float64(30 + i*10)}
	}
	fc := computeForecast(pts)
	// Last value is 70 > 50 → should be critical
	if fc.QueueStatus != "critical" {
		t.Errorf("expected critical queue status, got %s (last=%f)", fc.QueueStatus, pts[len(pts)-1].QueueDepth)
	}
}

// ---------- detectAnomalies ----------

func TestDetectAnomalies_NoAnomalyStable(t *testing.T) {
	pts := make([]*MetricsPoint, 20)
	for i := range pts {
		pts[i] = &MetricsPoint{Timestamp: int64(i * 60), FailureRate: 0.01}
	}
	anomalies := detectAnomalies(pts)
	if len(anomalies) != 0 {
		t.Errorf("expected no anomalies for stable data, got %d", len(anomalies))
	}
}

func TestDetectAnomalies_SpikeDetected(t *testing.T) {
	pts := make([]*MetricsPoint, 20)
	for i := range pts {
		pts[i] = &MetricsPoint{Timestamp: int64(i * 60), FailureRate: 0.01}
	}
	// Add spike in last 20% (idx 16-19)
	for i := 16; i < 20; i++ {
		pts[i].FailureRate = 0.9
	}
	anomalies := detectAnomalies(pts)
	if len(anomalies) == 0 {
		t.Error("expected anomaly for spike, got none")
	}
}

// ---------- MetricsCollector stop ----------

func TestMetricsCollectorStopIdempotent(t *testing.T) {
	mc := &MetricsCollector{done: make(chan struct{})}
	mc.Stop()
	mc.Stop() // should not panic or deadlock
}
