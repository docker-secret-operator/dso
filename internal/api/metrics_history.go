package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/execution"
)

// MetricsSnapshot represents a point-in-time metrics snapshot
type MetricsSnapshot struct {
	Timestamp         time.Time `json:"timestamp"`
	Throughput        float64   `json:"throughput"`
	SuccessRate       float64   `json:"success_rate"`
	FailureRate       float64   `json:"failure_rate"`
	QueueDepth        int       `json:"queue_depth"`
	WorkerUtilization float64   `json:"worker_utilization"`
	ActiveExecutions  int       `json:"active_executions"`
	CompletedCount    int       `json:"completed_count"`
	FailedCount       int       `json:"failed_count"`
}

// MetricsCollector collects and stores historical metrics
type MetricsCollector struct {
	dispatcher    *execution.Dispatcher
	workerManager *execution.WorkerManager
	mu            sync.RWMutex
	snapshots     []*MetricsSnapshot
	maxSnapshots  int
	ticker        *time.Ticker
	done          chan bool
	stopped       bool
	lastMetrics   *execution.DispatcherMetrics
	lastTime      time.Time
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(
	dispatcher *execution.Dispatcher,
	workerManager *execution.WorkerManager,
) *MetricsCollector {
	mc := &MetricsCollector{
		dispatcher:    dispatcher,
		workerManager: workerManager,
		snapshots:     make([]*MetricsSnapshot, 0, 10080), // 7 days at 1min intervals
		maxSnapshots:  10080,
		done:          make(chan bool),
		lastTime:      time.Now(),
	}

	// Start collection goroutine
	go mc.collectLoop()

	return mc
}

// collectLoop captures metrics every 60 seconds
func (mc *MetricsCollector) collectLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mc.captureSnapshot()
		case <-mc.done:
			return
		}
	}
}

// captureSnapshot captures current metrics
func (mc *MetricsCollector) captureSnapshot() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metrics := mc.dispatcher.GetMetrics(ctx)
	workers, _ := mc.workerManager.ListWorkers(ctx)

	// Calculate rates
	now := time.Now()
	throughput := metrics.ThroughputPerSec
	successRate := 0.0
	failureRate := 0.0

	totalExecuted := metrics.CompletedCount + metrics.FailedCount
	if totalExecuted > 0 {
		successRate = float64(metrics.CompletedCount) / float64(totalExecuted)
		failureRate = float64(metrics.FailedCount) / float64(totalExecuted)
	}

	// Calculate worker utilization
	workerUtilization := 0.0
	if len(workers) > 0 {
		totalCapacity := 0
		totalRunning := 0
		for _, w := range workers {
			totalCapacity += w.MaxConcurrent
			totalRunning += w.CurrentlyRunning
		}
		if totalCapacity > 0 {
			workerUtilization = float64(totalRunning) / float64(totalCapacity)
		}
	}

	snapshot := &MetricsSnapshot{
		Timestamp:         now,
		Throughput:        throughput,
		SuccessRate:       successRate,
		FailureRate:       failureRate,
		QueueDepth:        metrics.QueuedCount,
		WorkerUtilization: workerUtilization,
		ActiveExecutions:  metrics.ActiveCount,
		CompletedCount:    metrics.CompletedCount,
		FailedCount:       metrics.FailedCount,
	}

	mc.mu.Lock()
	mc.snapshots = append(mc.snapshots, snapshot)
	if len(mc.snapshots) > mc.maxSnapshots {
		// Keep only the last maxSnapshots
		mc.snapshots = mc.snapshots[len(mc.snapshots)-mc.maxSnapshots:]
	}
	mc.lastMetrics = &metrics
	mc.lastTime = now
	mc.mu.Unlock()
}

// GetHistory returns metrics history for a given period
func (mc *MetricsCollector) GetHistory(period time.Duration) []*MetricsSnapshot {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if len(mc.snapshots) == 0 {
		return []*MetricsSnapshot{}
	}

	cutoff := time.Now().Add(-period)
	result := make([]*MetricsSnapshot, 0)

	for _, snapshot := range mc.snapshots {
		if snapshot.Timestamp.After(cutoff) {
			result = append(result, snapshot)
		}
	}

	return result
}

// Stop stops the metrics collector (idempotent)
func (mc *MetricsCollector) Stop() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if !mc.stopped {
		mc.stopped = true
		close(mc.done)
	}
}

// MetricsHistoryHandler handles metrics history endpoints
type MetricsHistoryHandler struct {
	collector *MetricsCollector
}

// NewMetricsHistoryHandler creates a new metrics history handler
func NewMetricsHistoryHandler(collector *MetricsCollector) *MetricsHistoryHandler {
	return &MetricsHistoryHandler{
		collector: collector,
	}
}

// ServeHTTP handles metrics history requests
func (h *MetricsHistoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	periodStr := r.URL.Query().Get("period")
	if periodStr == "" {
		periodStr = "1h"
	}

	period, err := parsePeriod(periodStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("invalid period: %v", err),
		})
		return
	}

	history := h.collector.GetHistory(period)

	response := map[string]interface{}{
		"timestamp":  time.Now(),
		"period":     periodStr,
		"count":      len(history),
		"data":       history,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// parsePeriod parses period string to duration
func parsePeriod(period string) (time.Duration, error) {
	switch period {
	case "1h":
		return 1 * time.Hour, nil
	case "24h":
		return 24 * time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	default:
		// Try to parse as Go duration
		d, err := time.ParseDuration(period)
		if err != nil {
			return 0, fmt.Errorf("invalid period format: expected 1h, 24h, 7d, or valid duration")
		}
		return d, nil
	}
}
