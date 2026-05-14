package agent

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RotationTracer provides per-rotation tracing and diagnostics.
// Each rotation gets a unique trace ID that flows through all operations,
// making it easy to correlate logs and metrics.
type RotationTracer struct {
	traceID   string
	secret    string
	provider  string
	startTime time.Time
	logger    *zap.Logger
	events    []TraceEvent
	mu        sync.Mutex
}

// TraceEvent represents a single event in a rotation's lifecycle
type TraceEvent struct {
	Timestamp time.Time
	Level     string // info, warn, error
	Stage     string // create, health_check, swap, cleanup, etc.
	Message   string
	Duration  time.Duration
	Metadata  map[string]interface{}
}

// NewRotationTracer creates a tracer for a single rotation
func NewRotationTracer(secret, provider string, logger *zap.Logger) *RotationTracer {
	traceID := fmt.Sprintf("%s:%s:%d", secret, provider, time.Now().UnixNano())
	return &RotationTracer{
		traceID:   traceID,
		secret:    secret,
		provider:  provider,
		startTime: time.Now(),
		logger:    logger.With(zap.String("trace_id", traceID)),
		events:    make([]TraceEvent, 0),
	}
}

// TraceID returns the unique trace ID for this rotation
func (rt *RotationTracer) TraceID() string {
	return rt.traceID
}

// LogEvent logs a single event in the rotation lifecycle
func (rt *RotationTracer) LogEvent(stage, level, message string, duration time.Duration, metadata map[string]interface{}) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	event := TraceEvent{
		Timestamp: time.Now(),
		Level:     level,
		Stage:     stage,
		Message:   message,
		Duration:  duration,
		Metadata:  metadata,
	}

	rt.events = append(rt.events, event)

	// Log with appropriate level
	fields := []zap.Field{
		zap.String("stage", stage),
		zap.String("message", message),
		zap.Duration("duration", duration),
	}

	// Add metadata fields
	for k, v := range metadata {
		fields = append(fields, zap.Any(k, v))
	}

	switch level {
	case "error":
		rt.logger.Error("Rotation event", fields...)
	case "warn":
		rt.logger.Warn("Rotation event", fields...)
	default:
		rt.logger.Info("Rotation event", fields...)
	}
}

// Summary returns a summary of the rotation trace for reporting
func (rt *RotationTracer) Summary() map[string]interface{} {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	totalDuration := time.Since(rt.startTime)
	var errorCount, warnCount int
	var stages []string

	for _, event := range rt.events {
		stages = append(stages, event.Stage)
		if event.Level == "error" {
			errorCount++
		} else if event.Level == "warn" {
			warnCount++
		}
	}

	return map[string]interface{}{
		"trace_id":      rt.traceID,
		"secret":        rt.secret,
		"provider":      rt.provider,
		"total_duration": totalDuration,
		"event_count":    len(rt.events),
		"stages":         stages,
		"errors":         errorCount,
		"warnings":       warnCount,
	}
}

// HealthCheckDiagnostics provides diagnostic information about health check failures
type HealthCheckDiagnostics struct {
	ContainerID    string
	HealthStatus   string // starting, healthy, unhealthy
	ExitCode       int
	Output         string
	Timestamp      time.Time
	Duration       time.Duration
	Attempts       int
	LastError      string
}

// HealthCheckMonitor tracks health check attempts and provides diagnostics
type HealthCheckMonitor struct {
	diagnostics map[string]*HealthCheckDiagnostics
	mu          sync.RWMutex
	logger      *zap.Logger
}

// NewHealthCheckMonitor creates a monitor for health check diagnostics
func NewHealthCheckMonitor(logger *zap.Logger) *HealthCheckMonitor {
	return &HealthCheckMonitor{
		diagnostics: make(map[string]*HealthCheckDiagnostics),
		logger:      logger,
	}
}

// RecordHealthCheck records the result of a health check attempt
func (hcm *HealthCheckMonitor) RecordHealthCheck(containerID, status, output, lastError string, exitCode int, duration time.Duration) {
	hcm.mu.Lock()
	defer hcm.mu.Unlock()

	diag, exists := hcm.diagnostics[containerID]
	if !exists {
		diag = &HealthCheckDiagnostics{
			ContainerID: containerID,
		}
		hcm.diagnostics[containerID] = diag
	}

	diag.HealthStatus = status
	diag.ExitCode = exitCode
	diag.Output = output
	diag.LastError = lastError
	diag.Duration = duration
	diag.Timestamp = time.Now()
	diag.Attempts++

	// Log health check diagnostics
	hcm.logger.Info("Health check result",
		zap.String("container_id", containerID[:12]),
		zap.String("status", status),
		zap.Int("exit_code", exitCode),
		zap.Duration("duration", duration),
		zap.Int("attempts", diag.Attempts),
		zap.String("output", output))
}

// GetDiagnostics returns health check diagnostics for a container
func (hcm *HealthCheckMonitor) GetDiagnostics(containerID string) *HealthCheckDiagnostics {
	hcm.mu.RLock()
	defer hcm.mu.RUnlock()

	return hcm.diagnostics[containerID]
}

// ProviderLatencyMonitor tracks provider response times
type ProviderLatencyMonitor struct {
	latencies map[string][]time.Duration // provider -> latencies
	mu        sync.RWMutex
	logger    *zap.Logger
}

// NewProviderLatencyMonitor creates a latency monitor for providers
func NewProviderLatencyMonitor(logger *zap.Logger) *ProviderLatencyMonitor {
	return &ProviderLatencyMonitor{
		latencies: make(map[string][]time.Duration),
		logger:    logger,
	}
}

// RecordLatency records a provider request latency
func (plm *ProviderLatencyMonitor) RecordLatency(provider string, duration time.Duration) {
	plm.mu.Lock()
	defer plm.mu.Unlock()

	if _, exists := plm.latencies[provider]; !exists {
		plm.latencies[provider] = make([]time.Duration, 0)
	}

	plm.latencies[provider] = append(plm.latencies[provider], duration)

	// Keep only last 100 measurements per provider to avoid memory bloat
	if len(plm.latencies[provider]) > 100 {
		plm.latencies[provider] = plm.latencies[provider][1:]
	}

	// Log slow provider requests (>5s)
	if duration > 5*time.Second {
		plm.logger.Warn("Slow provider request",
			zap.String("provider", provider),
			zap.Duration("duration", duration))
	}
}

// GetStats returns latency statistics for a provider
func (plm *ProviderLatencyMonitor) GetStats(provider string) map[string]interface{} {
	plm.mu.RLock()
	defer plm.mu.RUnlock()

	latencies, exists := plm.latencies[provider]
	if !exists || len(latencies) == 0 {
		return map[string]interface{}{
			"provider":    provider,
			"count":       0,
			"average":     0,
			"min":         0,
			"max":         0,
			"p99":         0,
		}
	}

	// Calculate statistics
	var total time.Duration
	var min, max time.Duration = latencies[0], latencies[0]

	for _, lat := range latencies {
		total += lat
		if lat < min {
			min = lat
		}
		if lat > max {
			max = lat
		}
	}

	avg := total / time.Duration(len(latencies))

	return map[string]interface{}{
		"provider":    provider,
		"count":       len(latencies),
		"average":     avg,
		"min":         min,
		"max":         max,
		"last":        latencies[len(latencies)-1],
	}
}

// LockContentionMonitor tracks lock acquisition and contention
type LockContentionMonitor struct {
	acquisitions map[string]LockStatistic // secret -> stats
	mu           sync.RWMutex
	logger       *zap.Logger
}

// LockStatistic tracks lock statistics for a resource
type LockStatistic struct {
	Secret           string
	AcquisitionCount int
	TotalWaitTime    time.Duration
	MaxWaitTime      time.Duration
	MinWaitTime      time.Duration
	LastAcquisition  time.Time
	CurrentlyHeld    bool
}

// NewLockContentionMonitor creates a lock contention monitor
func NewLockContentionMonitor(logger *zap.Logger) *LockContentionMonitor {
	return &LockContentionMonitor{
		acquisitions: make(map[string]LockStatistic),
		logger:       logger,
	}
}

// RecordLockAcquisition records a lock acquisition event
func (lcm *LockContentionMonitor) RecordLockAcquisition(secret string, waitTime time.Duration) {
	lcm.mu.Lock()
	defer lcm.mu.Unlock()

	stat, exists := lcm.acquisitions[secret]
	if !exists {
		stat = LockStatistic{Secret: secret}
	}

	stat.AcquisitionCount++
	stat.TotalWaitTime += waitTime
	stat.LastAcquisition = time.Now()
	stat.CurrentlyHeld = true

	if !exists || waitTime > stat.MaxWaitTime {
		stat.MaxWaitTime = waitTime
	}
	if !exists || waitTime < stat.MinWaitTime || stat.MinWaitTime == 0 {
		stat.MinWaitTime = waitTime
	}

	lcm.acquisitions[secret] = stat

	// Log high contention (>1s wait time)
	if waitTime > 1*time.Second {
		lcm.logger.Warn("High lock contention detected",
			zap.String("secret", secret),
			zap.Duration("wait_time", waitTime))
	}
}

// RecordLockRelease records a lock release event
func (lcm *LockContentionMonitor) RecordLockRelease(secret string) {
	lcm.mu.Lock()
	defer lcm.mu.Unlock()

	if stat, exists := lcm.acquisitions[secret]; exists {
		stat.CurrentlyHeld = false
		lcm.acquisitions[secret] = stat
	}
}

// GetStats returns contention statistics for a secret
func (lcm *LockContentionMonitor) GetStats(secret string) *LockStatistic {
	lcm.mu.RLock()
	defer lcm.mu.RUnlock()

	if stat, exists := lcm.acquisitions[secret]; exists {
		return &stat
	}
	return nil
}
