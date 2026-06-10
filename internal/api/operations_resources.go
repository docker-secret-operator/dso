package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/docker-secret-operator/dso/internal/execution"
)

// ResourceMonitor handles resource monitoring endpoints
type ResourceMonitor struct {
	db             *sql.DB
	dispatcher     *execution.Dispatcher
	workerManager  *execution.WorkerManager
	executionQueue *execution.ExecutionQueue
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(
	db *sql.DB,
	dispatcher *execution.Dispatcher,
	workerManager *execution.WorkerManager,
	executionQueue *execution.ExecutionQueue,
) *ResourceMonitor {
	return &ResourceMonitor{
		db:             db,
		dispatcher:     dispatcher,
		workerManager:  workerManager,
		executionQueue: executionQueue,
	}
}

// ResourceResponse represents runtime resources
type ResourceResponse struct {
	Timestamp        time.Time      `json:"timestamp"`
	Memory           MemoryStats    `json:"memory"`
	Runtime          RuntimeStats   `json:"runtime"`
	Database         DatabaseStats  `json:"database"`
	Queue            QueueStats     `json:"queue"`
	Workers          WorkerStats    `json:"workers"`
	ExecutionMetrics ExecutionStats `json:"execution_metrics"`
}

// MemoryStats represents memory usage
type MemoryStats struct {
	AllocBytes      uint64  `json:"alloc_bytes"`
	TotalAllocBytes uint64  `json:"total_alloc_bytes"`
	SysBytes        uint64  `json:"sys_bytes"`
	HeapAllocBytes  uint64  `json:"heap_alloc_bytes"`
	HeapInuseBytes  uint64  `json:"heap_inuse_bytes"`
	AllocMB         float64 `json:"alloc_mb"`
	HeapAllocMB     float64 `json:"heap_alloc_mb"`
}

// RuntimeStats represents runtime statistics
type RuntimeStats struct {
	NumGoroutine int       `json:"num_goroutine"`
	GOMAXPROCS   int       `json:"gomaxprocs"`
	GCRuns       uint32    `json:"gc_runs"`
	LastGCTime   time.Time `json:"last_gc_time"`
	NextGCBytes  uint64    `json:"next_gc_bytes"`
}

// DatabaseStats represents database connection statistics
type DatabaseStats struct {
	OpenConnections int   `json:"open_connections"`
	InUse           int   `json:"in_use"`
	Idle            int   `json:"idle"`
	WaitCount       int64 `json:"wait_count"`
	WaitDuration    int64 `json:"wait_duration_ms"`
	MaxIdleTime     int64 `json:"max_idle_time_ms"`
}

// QueueStats represents queue statistics
type QueueStats struct {
	Depth           int     `json:"depth"`
	OldestItemAge   string  `json:"oldest_item_age"`
	OldestItemAgeMs int64   `json:"oldest_item_age_ms"`
	IncomingRate    float64 `json:"incoming_rate_per_sec"`
	CompletionRate  float64 `json:"completion_rate_per_sec"`
	AverageWaitTime string  `json:"average_wait_time"`
}

// WorkerStats represents worker statistics
type WorkerStats struct {
	TotalWorkers       int     `json:"total_workers"`
	HealthyWorkers     int     `json:"healthy_workers"`
	BusyWorkers        int     `json:"busy_workers"`
	IdleWorkers        int     `json:"idle_workers"`
	AverageUtilization float64 `json:"average_utilization"`
}

// ExecutionStats represents execution statistics
type ExecutionStats struct {
	Running   int `json:"running"`
	Queued    int `json:"queued"`
	Failed    int `json:"failed"`
	Completed int `json:"completed"`
}

// GetResources returns runtime resource statistics
func (rm *ResourceMonitor) GetResources(ctx context.Context) (*ResourceResponse, error) {
	resp := &ResourceResponse{
		Timestamp: time.Now(),
	}

	// Memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	resp.Memory = MemoryStats{
		AllocBytes:      m.Alloc,
		TotalAllocBytes: m.TotalAlloc,
		SysBytes:        m.Sys,
		HeapAllocBytes:  m.HeapAlloc,
		HeapInuseBytes:  m.HeapInuse,
		AllocMB:         float64(m.Alloc) / 1024 / 1024,
		HeapAllocMB:     float64(m.HeapAlloc) / 1024 / 1024,
	}

	// Runtime stats
	var lastGC time.Time
	if m.LastGC > 0 {
		lastGC = time.Unix(0, int64(m.LastGC))
	}
	resp.Runtime = RuntimeStats{
		NumGoroutine: runtime.NumGoroutine(),
		GOMAXPROCS:   runtime.GOMAXPROCS(-1),
		GCRuns:       m.NumGC,
		LastGCTime:   lastGC,
		NextGCBytes:  m.NextGC,
	}

	// Database stats
	if rm.db != nil {
		dbStats := rm.db.Stats()
		resp.Database = DatabaseStats{
			OpenConnections: dbStats.OpenConnections,
			InUse:           dbStats.InUse,
			Idle:            dbStats.Idle,
			WaitCount:       dbStats.WaitCount,
			WaitDuration:    int64(dbStats.WaitDuration.Milliseconds()),
			MaxIdleTime:     int64(dbStats.MaxIdleClosed),
		}
	}

	// Queue stats
	metrics := rm.dispatcher.GetMetrics(ctx)
	queueDepth := metrics.QueuedCount
	oldestAge := int64(0)
	oldestAgeStr := "N/A"

	if queueDepth > 0 {
		oldestItem, _ := rm.executionQueue.Peek(ctx)
		if oldestItem != nil {
			oldestAge = int64(time.Since(oldestItem.CreatedAt).Milliseconds())
			oldestAgeStr = time.Since(oldestItem.CreatedAt).String()
		}
	}

	throughput := metrics.ThroughputPerSec
	avgWaitTime := metrics.QueueWaitTime.String()

	resp.Queue = QueueStats{
		Depth:           queueDepth,
		OldestItemAge:   oldestAgeStr,
		OldestItemAgeMs: oldestAge,
		IncomingRate:    throughput,
		CompletionRate:  throughput,
		AverageWaitTime: avgWaitTime,
	}

	// Worker stats
	workers, _ := rm.workerManager.ListWorkers(ctx)
	healthyWorkers, _ := rm.workerManager.GetHealthyWorkers(ctx)
	busyCount := 0
	totalUtilization := 0.0

	for _, w := range workers {
		if w.CurrentlyRunning > 0 {
			busyCount++
		}
		if w.MaxConcurrent > 0 {
			totalUtilization += float64(w.CurrentlyRunning) / float64(w.MaxConcurrent)
		}
	}

	avgUtilization := 0.0
	if len(workers) > 0 {
		avgUtilization = totalUtilization / float64(len(workers))
	}

	resp.Workers = WorkerStats{
		TotalWorkers:       len(workers),
		HealthyWorkers:     len(healthyWorkers),
		BusyWorkers:        busyCount,
		IdleWorkers:        len(workers) - busyCount,
		AverageUtilization: avgUtilization,
	}

	// Execution metrics
	resp.ExecutionMetrics = ExecutionStats{
		Running:   metrics.ActiveCount,
		Queued:    metrics.QueuedCount,
		Failed:    metrics.FailedCount,
		Completed: metrics.CompletedCount,
	}

	return resp, nil
}

// ServeHTTP handles resource monitoring requests
func (rm *ResourceMonitor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resources, err := rm.GetResources(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resources)
}
