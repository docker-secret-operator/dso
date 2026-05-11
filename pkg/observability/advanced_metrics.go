package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Provider Health Metrics
	ProviderHealthCheck = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dso_provider_health_check_status",
			Help: "Provider health check status (1=healthy, 0=unhealthy)",
		},
		[]string{"provider"},
	)

	ProviderHeartbeatLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_provider_heartbeat_latency_seconds",
			Help:    "Provider heartbeat response latency",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		},
		[]string{"provider"},
	)

	ProviderRestartDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_provider_restart_duration_seconds",
			Help:    "Time taken to restart provider process",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		},
		[]string{"provider"},
	)

	// Reconnection Metrics
	ReconnectDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_reconnect_duration_seconds",
			Help:    "Time taken to reconnect to daemon",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{"reason"},
	)

	ReconnectAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_reconnect_attempts_total",
			Help: "Total number of reconnection attempts",
		},
		[]string{"reason", "status"},
	)

	// Reconciliation Metrics
	ReconciliationDrift = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dso_reconciliation_drift_seconds",
			Help: "Time since last successful reconciliation",
		},
		[]string{"component"},
	)

	ReconciliationItemsProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_reconciliation_items_processed_total",
			Help: "Total items processed during reconciliation",
		},
		[]string{"component", "status"},
	)

	ReconciliationErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_reconciliation_errors_total",
			Help: "Total reconciliation errors",
		},
		[]string{"component", "error_type"},
	)

	// Stale Cleanup Metrics
	StaleItemsDetected = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_stale_items_detected_total",
			Help: "Total stale items detected and cleaned",
		},
		[]string{"item_type"},
	)

	StaleCleanupDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_stale_cleanup_duration_seconds",
			Help:    "Time taken to cleanup stale items",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1},
		},
		[]string{"item_type"},
	)

	// Queue Metrics (Advanced)
	QueueProcessingLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_queue_processing_latency_seconds",
			Help:    "Latency to process item from queue",
			Buckets: []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1},
		},
		[]string{"queue_type"},
	)

	QueueEnqueueLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_queue_enqueue_latency_seconds",
			Help:    "Latency to enqueue item",
			Buckets: []float64{0.0001, 0.001, 0.01, 0.05},
		},
		[]string{"queue_type"},
	)

	QueueRejectRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dso_queue_reject_rate",
			Help: "Current queue rejection rate (0-1)",
		},
		[]string{"queue_type"},
	)

	// Worker Pool Metrics
	WorkerPoolUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dso_worker_pool_utilization_percent",
			Help: "Worker pool utilization percentage (0-100)",
		},
		[]string{"pool_name"},
	)

	WorkerPoolStarvation = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_worker_pool_starvation_total",
			Help: "Times worker pool was fully saturated",
		},
		[]string{"pool_name"},
	)

	WorkerTaskDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_worker_task_duration_seconds",
			Help:    "Time for worker to complete task",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		},
		[]string{"pool_name", "task_type"},
	)

	// Duplicate Event Suppression
	DuplicateEventsSuppressed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_duplicate_events_suppressed_total",
			Help: "Total duplicate events suppressed by deduplication",
		},
		[]string{"reason"},
	)

	DedupCacheMissRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dso_dedup_cache_miss_rate",
			Help: "Deduplication cache miss rate (0-1)",
		},
		[]string{},
	)

	// Recovery Metrics
	RecoveryOperationsRetried = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_recovery_operations_retried_total",
			Help: "Total operations retried during recovery",
		},
		[]string{"operation_type"},
	)

	RecoveryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_recovery_duration_seconds",
			Help:    "Time to complete recovery after failure",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60},
		},
		[]string{"failure_type"},
	)

	// State Consistency Metrics
	StateInconsistenciesDetected = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_state_inconsistencies_detected_total",
			Help: "Total state inconsistencies detected",
		},
		[]string{"inconsistency_type"},
	)

	StateRepairDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_state_repair_duration_seconds",
			Help:    "Time to repair detected state inconsistency",
			Buckets: []float64{0.1, 0.5, 1, 2, 5},
		},
		[]string{"inconsistency_type"},
	)

	// Memory and Resource Tracking
	RuntimeMemoryUsageBytes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "dso_runtime_memory_usage_bytes",
			Help: "Current runtime memory usage",
		},
	)

	RuntimeGoroutineCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "dso_runtime_goroutine_count",
			Help: "Current number of goroutines",
		},
	)

	RuntimeMemoryAllocRate = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "dso_runtime_memory_alloc_rate",
			Help: "Memory allocation rate (bytes/sec)",
		},
	)

	// Event Stream Metrics
	EventStreamLag = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dso_event_stream_lag_milliseconds",
			Help: "Lag between event generation and processing",
		},
		[]string{"stream_type"},
	)

	EventStreamInterruptions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_event_stream_interruptions_total",
			Help: "Total event stream interruptions",
		},
		[]string{"reason"},
	)

	// Injection Performance Metrics
	InjectionAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_injection_attempts_total",
			Help: "Total injection attempts",
		},
		[]string{"injection_type", "status"},
	)

	InjectionLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dso_injection_latency_seconds",
			Help:    "Latency of secret injection operation",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2},
		},
		[]string{"injection_type"},
	)

	InjectionVerificationFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_injection_verification_failures_total",
			Help: "Total injection verification failures",
		},
		[]string{"reason"},
	)
)

// RecordReconnectDuration records a reconnection operation
func RecordReconnectDuration(reason string, duration float64) {
	ReconnectDuration.WithLabelValues(reason).Observe(duration)
}

// RecordProviderRestart records provider restart operation
func RecordProviderRestart(provider string, duration float64) {
	ProviderRestartDuration.WithLabelValues(provider).Observe(duration)
}

// RecordStaleItemsCleanup records stale item cleanup
func RecordStaleItemsCleanup(itemType string, count int, duration float64) {
	StaleItemsDetected.WithLabelValues(itemType).Add(float64(count))
	StaleCleanupDuration.WithLabelValues(itemType).Observe(duration)
}

// RecordReconciliationItem records item processed during reconciliation
func RecordReconciliationItem(component, status string) {
	ReconciliationItemsProcessed.WithLabelValues(component, status).Inc()
}

// RecordDuplicateEventSuppressed records duplicate event suppression
func RecordDuplicateEventSuppressed(reason string) {
	DuplicateEventsSuppressed.WithLabelValues(reason).Inc()
}

// RecordInjectionAttempt records injection attempt
func RecordInjectionAttempt(injectionType, status string) {
	InjectionAttempts.WithLabelValues(injectionType, status).Inc()
}

// RecordInjectionLatency records injection latency
func RecordInjectionLatency(injectionType string, duration float64) {
	InjectionLatency.WithLabelValues(injectionType).Observe(duration)
}
