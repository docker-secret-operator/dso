package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type MetricsPoint struct {
	Timestamp         int64   `json:"ts"` // Unix seconds
	SuccessRate       float64 `json:"sr"`
	FailureRate       float64 `json:"fr"`
	Throughput        float64 `json:"tp"`
	QueueDepth        float64 `json:"qd"`
	WorkerUtilization float64 `json:"wu"`
	ActiveExecutions  float64 `json:"ae"`
	MemoryMB          float64 `json:"mm"`
	Goroutines        float64 `json:"gr"`
}

type TrendInfo struct {
	Direction string  `json:"direction"` // "improving" | "stable" | "degrading"
	Arrow     string  `json:"arrow"`
	Slope     float64 `json:"slope"`
	MovingAvg float64 `json:"moving_avg"`
}

type TrendReport struct {
	SuccessRate       TrendInfo `json:"success_rate"`
	FailureRate       TrendInfo `json:"failure_rate"`
	QueueDepth        TrendInfo `json:"queue_depth"`
	WorkerUtilization TrendInfo `json:"worker_utilization"`
	MemoryMB          TrendInfo `json:"memory_mb"`
}

type ForecastResult struct {
	QueueSaturationHours  float64 `json:"queue_saturation_hours"`  // -1 = no saturation predicted
	WorkerExhaustionHours float64 `json:"worker_exhaustion_hours"` // -1 = no exhaustion predicted
	QueueStatus           string  `json:"queue_status"`            // healthy | warning | critical
	WorkerStatus          string  `json:"worker_status"`
}

type AnomalyInfo struct {
	Field     string    `json:"field"`
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
	Baseline  float64   `json:"baseline"`
	Deviation float64   `json:"deviation"`
	Message   string    `json:"message"`
}

type MetricsHistoryResponse struct {
	Period      string          `json:"period"`
	Granularity string          `json:"granularity"`
	Count       int             `json:"count"`
	Data        []*MetricsPoint `json:"data"`
	Trends      TrendReport     `json:"trends"`
	Forecast    ForecastResult  `json:"forecast"`
	Anomalies   []AnomalyInfo   `json:"anomalies"`
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

type MetricsAPIHandler struct {
	db        *sql.DB
	collector *MetricsCollector
}

func NewMetricsAPIHandler(db *sql.DB, collector *MetricsCollector) *MetricsAPIHandler {
	return &MetricsAPIHandler{db: db, collector: collector}
}

func (h *MetricsAPIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/metrics/history":
		h.handleHistory(w, r)
	case r.URL.Path == "/api/metrics/export":
		h.handleExport(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// FG3 — GET /api/metrics/history
// ---------------------------------------------------------------------------

func (h *MetricsAPIHandler) handleHistory(w http.ResponseWriter, r *http.Request) {
	periodStr := r.URL.Query().Get("period")
	if periodStr == "" {
		periodStr = "24h"
	}
	granularityStr := r.URL.Query().Get("granularity")
	if granularityStr == "" {
		granularityStr = defaultGranularity(periodStr)
	}

	period, err := parsePeriod(periodStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	granularity, err := parseGranularity(granularityStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	raw, err := h.querySnapshots(period)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	aggregated := aggregateByGranularity(raw, granularity)

	trends := computeTrends(aggregated)
	forecast := computeForecast(aggregated)
	anomalies := detectAnomalies(aggregated)

	resp := MetricsHistoryResponse{
		Period:      periodStr,
		Granularity: granularityStr,
		Count:       len(aggregated),
		Data:        aggregated,
		Trends:      trends,
		Forecast:    forecast,
		Anomalies:   anomalies,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ---------------------------------------------------------------------------
// FG9 — GET /api/metrics/export
// ---------------------------------------------------------------------------

func (h *MetricsAPIHandler) handleExport(w http.ResponseWriter, r *http.Request) {
	periodStr := r.URL.Query().Get("period")
	if periodStr == "" {
		periodStr = "24h"
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format != "csv" {
		format = "json"
	}

	period, err := parsePeriod(periodStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	raw, err := h.querySnapshots(period)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="metrics_%s.csv"`, periodStr))

		var buf bytes.Buffer
		cw := csv.NewWriter(&buf)
		_ = cw.Write([]string{
			"timestamp", "success_rate", "failure_rate", "throughput",
			"queue_depth", "worker_utilization", "active_executions",
			"memory_mb", "goroutines",
		})
		for _, p := range raw {
			_ = cw.Write([]string{
				time.Unix(p.Timestamp, 0).UTC().Format(time.RFC3339),
				strconv.FormatFloat(p.SuccessRate, 'f', 4, 64),
				strconv.FormatFloat(p.FailureRate, 'f', 4, 64),
				strconv.FormatFloat(p.Throughput, 'f', 4, 64),
				strconv.FormatFloat(p.QueueDepth, 'f', 0, 64),
				strconv.FormatFloat(p.WorkerUtilization, 'f', 4, 64),
				strconv.FormatFloat(p.ActiveExecutions, 'f', 0, 64),
				strconv.FormatFloat(p.MemoryMB, 'f', 2, 64),
				strconv.FormatFloat(p.Goroutines, 'f', 0, 64),
			})
		}
		cw.Flush()
		w.Write(buf.Bytes())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="metrics_%s.json"`, periodStr))
	json.NewEncoder(w).Encode(map[string]interface{}{
		"period":    periodStr,
		"exported":  time.Now().UTC(),
		"count":     len(raw),
		"snapshots": raw,
	})
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (h *MetricsAPIHandler) querySnapshots(period time.Duration) ([]*MetricsPoint, error) {
	if h.db == nil {
		return []*MetricsPoint{}, nil
	}
	cutoff := time.Now().Add(-period)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rows, err := h.db.QueryContext(ctx,
		`SELECT timestamp, success_rate, failure_rate, throughput, queue_depth,
			worker_utilization, active_executions, memory_mb, goroutines
		 FROM metrics_history WHERE timestamp > ? ORDER BY timestamp ASC`,
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*MetricsPoint
	for rows.Next() {
		var ts time.Time
		p := &MetricsPoint{}
		if err := rows.Scan(&ts, &p.SuccessRate, &p.FailureRate, &p.Throughput,
			&p.QueueDepth, &p.WorkerUtilization, &p.ActiveExecutions,
			&p.MemoryMB, &p.Goroutines); err != nil {
			continue
		}
		p.Timestamp = ts.Unix()
		result = append(result, p)
	}
	return result, rows.Err()
}

func defaultGranularity(period string) string {
	switch period {
	case "1h":
		return "1m"
	case "24h":
		return "5m"
	case "7d":
		return "1h"
	default:
		return "1h"
	}
}

func parseGranularity(g string) (time.Duration, error) {
	switch g {
	case "1m":
		return time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	default:
		d, err := time.ParseDuration(g)
		if err != nil {
			return 0, fmt.Errorf("invalid granularity: expected 1m, 5m, 1h")
		}
		return d, nil
	}
}

// aggregateByGranularity averages snapshots into time buckets
func aggregateByGranularity(points []*MetricsPoint, g time.Duration) []*MetricsPoint {
	if len(points) == 0 {
		return []*MetricsPoint{}
	}
	buckets := make(map[int64][]*MetricsPoint)
	bucketSecs := int64(g.Seconds())

	for _, p := range points {
		bucket := (p.Timestamp / bucketSecs) * bucketSecs
		buckets[bucket] = append(buckets[bucket], p)
	}

	result := make([]*MetricsPoint, 0, len(buckets))
	for bucket, pts := range buckets {
		agg := &MetricsPoint{Timestamp: bucket}
		for _, p := range pts {
			agg.SuccessRate += p.SuccessRate
			agg.FailureRate += p.FailureRate
			agg.Throughput += p.Throughput
			agg.QueueDepth += p.QueueDepth
			agg.WorkerUtilization += p.WorkerUtilization
			agg.ActiveExecutions += p.ActiveExecutions
			agg.MemoryMB += p.MemoryMB
			agg.Goroutines += p.Goroutines
		}
		n := float64(len(pts))
		agg.SuccessRate /= n
		agg.FailureRate /= n
		agg.Throughput /= n
		agg.QueueDepth /= n
		agg.WorkerUtilization /= n
		agg.ActiveExecutions /= n
		agg.MemoryMB /= n
		agg.Goroutines /= n
		result = append(result, agg)
	}

	// Sort by timestamp
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j].Timestamp < result[j-1].Timestamp; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	return result
}

// FG5 — trend analysis: linear regression slope + moving average
func computeTrends(points []*MetricsPoint) TrendReport {
	extract := func(pts []*MetricsPoint, f func(*MetricsPoint) float64) TrendInfo {
		vals := make([]float64, len(pts))
		for i, p := range pts {
			vals[i] = f(p)
		}
		slope := linearSlope(vals)
		ma := movingAverage(vals, 5)
		return TrendInfo{
			Direction: slopeDirection(slope),
			Arrow:     slopeArrow(slope),
			Slope:     math.Round(slope*10000) / 10000,
			MovingAvg: math.Round(ma*100) / 100,
		}
	}

	srTrend := extract(points, func(p *MetricsPoint) float64 { return p.SuccessRate })
	frTrend := extract(points, func(p *MetricsPoint) float64 { return p.FailureRate })
	// For success_rate, positive slope is "improving"; for failure_rate, positive slope is "degrading"
	if srTrend.Slope > 0.0001 {
		srTrend.Direction = "improving"
		srTrend.Arrow = "↑"
	} else if srTrend.Slope < -0.0001 {
		srTrend.Direction = "degrading"
		srTrend.Arrow = "↓"
	}
	if frTrend.Slope > 0.0001 {
		frTrend.Direction = "degrading"
		frTrend.Arrow = "↑"
	} else if frTrend.Slope < -0.0001 {
		frTrend.Direction = "improving"
		frTrend.Arrow = "↓"
	}

	return TrendReport{
		SuccessRate:       srTrend,
		FailureRate:       frTrend,
		QueueDepth:        extract(points, func(p *MetricsPoint) float64 { return p.QueueDepth }),
		WorkerUtilization: extract(points, func(p *MetricsPoint) float64 { return p.WorkerUtilization }),
		MemoryMB:          extract(points, func(p *MetricsPoint) float64 { return p.MemoryMB }),
	}
}

// FG6 — capacity forecasting based on last 24 h trend
func computeForecast(points []*MetricsPoint) ForecastResult {
	res := ForecastResult{
		QueueSaturationHours:  -1,
		WorkerExhaustionHours: -1,
		QueueStatus:           "healthy",
		WorkerStatus:          "healthy",
	}
	if len(points) < 2 {
		return res
	}

	queueVals := make([]float64, len(points))
	workerVals := make([]float64, len(points))
	for i, p := range points {
		queueVals[i] = p.QueueDepth
		workerVals[i] = p.WorkerUtilization
	}

	queueSlope := linearSlope(queueVals)   // units/snapshot
	workerSlope := linearSlope(workerVals) // 0-1/snapshot

	// Convert slope to per-hour (snapshots are ~1m or ~5m or ~1h apart; use point count / period estimate)
	// Use the actual time span between first and last point
	if len(points) >= 2 {
		spanSecs := float64(points[len(points)-1].Timestamp - points[0].Timestamp)
		if spanSecs > 0 {
			snapshotInterval := spanSecs / float64(len(points)-1) / 3600 // hours per snapshot
			if snapshotInterval > 0 {
				queueSlopePerHour := queueSlope / snapshotInterval
				workerSlopePerHour := workerSlope / snapshotInterval

				lastQueue := queueVals[len(queueVals)-1]
				lastWorker := workerVals[len(workerVals)-1]

				// Forecast hours until queue grows beyond 100 (arbitrary saturation threshold)
				const queueSaturation = 100.0
				if queueSlopePerHour > 0 && lastQueue < queueSaturation {
					res.QueueSaturationHours = (queueSaturation - lastQueue) / queueSlopePerHour
				}
				// Forecast hours until worker utilization hits 0.95
				const workerSaturation = 0.95
				if workerSlopePerHour > 0 && lastWorker < workerSaturation {
					res.WorkerExhaustionHours = (workerSaturation - lastWorker) / workerSlopePerHour
				}
			}
		}
	}

	// Determine status from last values + forecast
	lastQueue := queueVals[len(queueVals)-1]
	lastWorker := workerVals[len(workerVals)-1]

	if lastQueue > 50 || (res.QueueSaturationHours > 0 && res.QueueSaturationHours < 4) {
		res.QueueStatus = "critical"
	} else if lastQueue > 20 || (res.QueueSaturationHours > 0 && res.QueueSaturationHours < 24) {
		res.QueueStatus = "warning"
	}

	if lastWorker > 0.9 || (res.WorkerExhaustionHours > 0 && res.WorkerExhaustionHours < 4) {
		res.WorkerStatus = "critical"
	} else if lastWorker > 0.7 || (res.WorkerExhaustionHours > 0 && res.WorkerExhaustionHours < 24) {
		res.WorkerStatus = "warning"
	}

	return res
}

// FG7 — anomaly detection: value > mean + 2σ or < mean - 2σ
func detectAnomalies(points []*MetricsPoint) []AnomalyInfo {
	var anomalies []AnomalyInfo
	if len(points) < 10 {
		return anomalies
	}

	type series struct {
		name  string
		vals  []float64
		check func(val, baseline, stddev float64) (bool, string)
	}

	// threshold returns true if v is an outlier: either σ-based (when stddev > floor)
	// or ratio-based when the baseline is nearly constant.
	outlier := func(v, baseline, stddev, minDelta, minRatio float64) bool {
		floor := baseline * 0.05
		if floor < minDelta {
			floor = minDelta
		}
		if stddev > floor {
			return v > baseline+2*stddev
		}
		// stddev ≈ 0: flag if value exceeds minRatio × baseline
		return v > baseline*minRatio && v-baseline > minDelta
	}

	seriesList := []series{
		{
			name: "failure_rate",
			vals: mapFloats(points, func(p *MetricsPoint) float64 { return p.FailureRate }),
			check: func(v, b, s float64) (bool, string) {
				return outlier(v, b, s, 0.05, 3.0), "Error rate spike"
			},
		},
		{
			name: "queue_depth",
			vals: mapFloats(points, func(p *MetricsPoint) float64 { return p.QueueDepth }),
			check: func(v, b, s float64) (bool, string) {
				return outlier(v, b, s, 5.0, 2.0), "Queue depth spike"
			},
		},
		{
			name: "memory_mb",
			vals: mapFloats(points, func(p *MetricsPoint) float64 { return p.MemoryMB }),
			check: func(v, b, s float64) (bool, string) {
				return b > 0 && outlier(v, b, s, b*0.2, 1.2), "Memory usage spike"
			},
		},
		{
			name: "worker_utilization",
			vals: mapFloats(points, func(p *MetricsPoint) float64 { return p.WorkerUtilization }),
			check: func(v, b, s float64) (bool, string) {
				return v > 0.95 && outlier(v, b, s, 0.1, 1.5), "Worker exhaustion"
			},
		},
	}

	// Only check the last 20% of points against baseline from the earlier 80%
	splitIdx := len(points) * 4 / 5
	if splitIdx < 5 {
		return anomalies
	}

	for _, ser := range seriesList {
		baseline := ser.vals[:splitIdx]
		mean := avg(baseline)
		stddev := stdDev(baseline)

		for i := splitIdx; i < len(points); i++ {
			v := ser.vals[i]
			if triggered, msg := ser.check(v, mean, stddev); triggered {
				anomalies = append(anomalies, AnomalyInfo{
					Field:     ser.name,
					Timestamp: time.Unix(points[i].Timestamp, 0),
					Value:     math.Round(v*1000) / 1000,
					Baseline:  math.Round(mean*1000) / 1000,
					Deviation: math.Round((v-mean)*1000) / 1000,
					Message:   msg,
				})
			}
		}
	}
	return anomalies
}

// --------------------------------------------------------------------------
// Math helpers
// --------------------------------------------------------------------------

func linearSlope(vals []float64) float64 {
	n := float64(len(vals))
	if n < 2 {
		return 0
	}
	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0
	for i, v := range vals {
		x := float64(i)
		sumX += x
		sumY += v
		sumXY += x * v
		sumX2 += x * x
	}
	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}

func movingAverage(vals []float64, window int) float64 {
	if len(vals) == 0 {
		return 0
	}
	start := len(vals) - window
	if start < 0 {
		start = 0
	}
	sum := 0.0
	for _, v := range vals[start:] {
		sum += v
	}
	return sum / float64(len(vals[start:]))
}

func slopeDirection(slope float64) string {
	if slope > 0.0001 {
		return "improving"
	}
	if slope < -0.0001 {
		return "degrading"
	}
	return "stable"
}

func slopeArrow(slope float64) string {
	if slope > 0.0001 {
		return "↑"
	}
	if slope < -0.0001 {
		return "↓"
	}
	return "→"
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func stdDev(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	mean := avg(vals)
	sum := 0.0
	for _, v := range vals {
		d := v - mean
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(vals)))
}

func mapFloats(pts []*MetricsPoint, f func(*MetricsPoint) float64) []float64 {
	out := make([]float64, len(pts))
	for i, p := range pts {
		out[i] = f(p)
	}
	return out
}
