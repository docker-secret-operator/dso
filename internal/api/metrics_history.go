package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/execution"
)

const metricsRetentionDays = 30

// MetricsSnapshot represents a point-in-time metrics snapshot
type MetricsSnapshot struct {
	Timestamp         time.Time `json:"timestamp"`
	Throughput        float64   `json:"throughput"`
	SuccessRate       float64   `json:"success_rate"`
	FailureRate       float64   `json:"failure_rate"`
	QueueDepth        int       `json:"queue_depth"`
	WorkerUtilization float64   `json:"worker_utilization"`
	ActiveExecutions  int       `json:"active_executions"`
	MemoryMB          float64   `json:"memory_mb"`
	Goroutines        int       `json:"goroutines"`
	CompletedCount    int       `json:"completed_count"`
	FailedCount       int       `json:"failed_count"`
}

// MetricsCollector collects and stores historical metrics in SQLite
type MetricsCollector struct {
	dispatcher    *execution.Dispatcher
	workerManager *execution.WorkerManager
	db            *sql.DB
	mu            sync.Mutex
	done          chan struct{}
	stopped       bool
}

// NewMetricsCollector creates a new metrics collector.
// db may be nil — in that case metrics are collected but not persisted.
func NewMetricsCollector(
	dispatcher *execution.Dispatcher,
	workerManager *execution.WorkerManager,
) *MetricsCollector {
	mc := &MetricsCollector{
		dispatcher:    dispatcher,
		workerManager: workerManager,
		done:          make(chan struct{}),
	}
	go mc.collectLoop()
	return mc
}

// SetDB attaches a database connection for persistence.
// Must be called before the first snapshot (call immediately after NewMetricsCollector).
func (mc *MetricsCollector) SetDB(db *sql.DB) {
	mc.mu.Lock()
	mc.db = db
	mc.mu.Unlock()
}

// collectLoop captures metrics every 60 seconds and purges old data daily.
func (mc *MetricsCollector) collectLoop() {
	ticker := time.NewTicker(60 * time.Second)
	cleanup := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	defer cleanup.Stop()

	for {
		select {
		case <-ticker.C:
			mc.captureSnapshot()
		case <-cleanup.C:
			mc.purgeOldSnapshots()
		case <-mc.done:
			return
		}
	}
}

// captureSnapshot captures current system and execution metrics and persists them.
func (mc *MetricsCollector) captureSnapshot() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metrics := mc.dispatcher.GetMetrics(ctx)
	workers, _ := mc.workerManager.ListWorkers(ctx)

	now := time.Now()

	successRate := 0.0
	failureRate := 0.0
	total := metrics.CompletedCount + metrics.FailedCount
	if total > 0 {
		successRate = float64(metrics.CompletedCount) / float64(total)
		failureRate = float64(metrics.FailedCount) / float64(total)
	}

	workerUtilization := 0.0
	if len(workers) > 0 {
		totalCap, totalRunning := 0, 0
		for _, w := range workers {
			totalCap += w.MaxConcurrent
			totalRunning += w.CurrentlyRunning
		}
		if totalCap > 0 {
			workerUtilization = float64(totalRunning) / float64(totalCap)
		}
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memMB := float64(memStats.Alloc) / (1024 * 1024)
	goroutines := runtime.NumGoroutine()

	snap := &MetricsSnapshot{
		Timestamp:         now,
		Throughput:        metrics.ThroughputPerSec,
		SuccessRate:       successRate,
		FailureRate:       failureRate,
		QueueDepth:        metrics.QueuedCount,
		WorkerUtilization: workerUtilization,
		ActiveExecutions:  metrics.ActiveCount,
		MemoryMB:          memMB,
		Goroutines:        goroutines,
		CompletedCount:    metrics.CompletedCount,
		FailedCount:       metrics.FailedCount,
	}

	mc.mu.Lock()
	db := mc.db
	mc.mu.Unlock()

	if db != nil {
		insertCtx, insertCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer insertCancel()
		_, _ = db.ExecContext(insertCtx,
			`INSERT INTO metrics_history
				(timestamp, success_rate, failure_rate, throughput, queue_depth,
				 worker_utilization, active_executions, memory_mb, goroutines)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			snap.Timestamp, snap.SuccessRate, snap.FailureRate, snap.Throughput,
			snap.QueueDepth, snap.WorkerUtilization, snap.ActiveExecutions,
			snap.MemoryMB, snap.Goroutines,
		)
	}
}

// purgeOldSnapshots deletes snapshots older than the retention period.
func (mc *MetricsCollector) purgeOldSnapshots() {
	mc.mu.Lock()
	db := mc.db
	mc.mu.Unlock()

	if db == nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -metricsRetentionDays)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = db.ExecContext(ctx, `DELETE FROM metrics_history WHERE timestamp < ?`, cutoff)
}

// GetHistory queries snapshots from SQLite for the given period.
func (mc *MetricsCollector) GetHistory(period time.Duration) ([]*MetricsSnapshot, error) {
	mc.mu.Lock()
	db := mc.db
	mc.mu.Unlock()

	if db == nil {
		return []*MetricsSnapshot{}, nil
	}

	cutoff := time.Now().Add(-period)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx,
		`SELECT timestamp, success_rate, failure_rate, throughput, queue_depth,
			worker_utilization, active_executions, memory_mb, goroutines
		 FROM metrics_history WHERE timestamp > ? ORDER BY timestamp ASC`,
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*MetricsSnapshot
	for rows.Next() {
		s := &MetricsSnapshot{}
		if err := rows.Scan(
			&s.Timestamp, &s.SuccessRate, &s.FailureRate, &s.Throughput,
			&s.QueueDepth, &s.WorkerUtilization, &s.ActiveExecutions,
			&s.MemoryMB, &s.Goroutines,
		); err != nil {
			continue
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

// Stop stops the metrics collector (idempotent).
func (mc *MetricsCollector) Stop() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if !mc.stopped {
		mc.stopped = true
		close(mc.done)
	}
}

// --------------------------------------------------------------------------
// Legacy in-process handler (used by /api/operations/metrics-history)
// --------------------------------------------------------------------------

// MetricsHistoryHandler wraps the collector for the existing route
type MetricsHistoryHandler struct {
	collector *MetricsCollector
}

func NewMetricsHistoryHandler(collector *MetricsCollector) *MetricsHistoryHandler {
	return &MetricsHistoryHandler{collector: collector}
}

func (h *MetricsHistoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	periodStr := r.URL.Query().Get("period")
	if periodStr == "" {
		periodStr = "1h"
	}
	period, err := parsePeriod(periodStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("invalid period: %v", err)})
		return
	}

	history, _ := h.collector.GetHistory(period)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"timestamp": time.Now(),
		"period":    periodStr,
		"count":     len(history),
		"data":      history,
	})
}

func parsePeriod(period string) (time.Duration, error) {
	switch period {
	case "1h":
		return 1 * time.Hour, nil
	case "24h":
		return 24 * time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	case "30d":
		return 30 * 24 * time.Hour, nil
	default:
		d, err := time.ParseDuration(period)
		if err != nil {
			return 0, fmt.Errorf("invalid period: expected 1h, 24h, 7d, 30d")
		}
		return d, nil
	}
}
