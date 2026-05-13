package providers

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

var (
	// ProviderRestarts tracks restart attempts
	ProviderRestarts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_provider_restarts_total",
			Help: "Total number of provider process restarts",
		},
		[]string{"provider", "reason"},
	)

	// ProviderCrashes tracks crash occurrences
	ProviderCrashes = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_provider_crashes_total",
			Help: "Total number of provider process crashes",
		},
		[]string{"provider"},
	)

	// ProviderUptime tracks provider process uptime
	ProviderUptime = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dso_provider_uptime_seconds",
			Help: "Current provider process uptime in seconds",
		},
		[]string{"provider"},
	)

	// ProviderHealth tracks provider health status (1=healthy, 0=unhealthy)
	ProviderHealth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dso_provider_health_status",
			Help: "Provider health status (1=healthy, 0=unhealthy)",
		},
		[]string{"provider"},
	)

	// ProviderHeartbeatFailures tracks heartbeat failures
	ProviderHeartbeatFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_provider_heartbeat_failures_total",
			Help: "Total number of provider heartbeat failures",
		},
		[]string{"provider"},
	)
)

// ProviderHealthState represents the health status of a provider
type ProviderHealthState int32

const (
	HealthUnknown ProviderHealthState = iota
	HealthHealthy
	HealthDegraded
	HealthUnhealthy
)

// ProviderSupervisor monitors and manages provider process lifecycle
type ProviderSupervisor struct {
	logger                 *zap.Logger
	providerName           string
	startTime              time.Time
	lastHeartbeat          time.Time
	consecutiveCrashes     int32
	maxConsecutiveCrashes  int32
	consecutiveFailures    int32
	maxConsecutiveFailures int32
	health                 atomic.Int32 // ProviderHealthState
	mu                     sync.RWMutex
	heartbeatTimeout       time.Duration
	restartBackoff         time.Duration
	maxRestartBackoff      time.Duration
	restartCount           int32
	crashCount             int32
}

// NewProviderSupervisor creates a new provider supervisor
func NewProviderSupervisor(logger *zap.Logger, providerName string) *ProviderSupervisor {
	ps := &ProviderSupervisor{
		logger:                 logger,
		providerName:           providerName,
		startTime:              time.Now(),
		lastHeartbeat:          time.Now(),
		maxConsecutiveCrashes:  3,
		maxConsecutiveFailures: 5,
		heartbeatTimeout:       10 * time.Second,
		restartBackoff:         time.Second,
		maxRestartBackoff:      30 * time.Second,
	}
	ps.health.Store(int32(HealthUnknown))
	return ps
}

// MarkHealthy marks the provider as healthy and resets failure counters
func (ps *ProviderSupervisor) MarkHealthy() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.lastHeartbeat = time.Now()
	ps.consecutiveCrashes = 0
	ps.consecutiveFailures = 0
	ps.health.Store(int32(HealthHealthy))

	ps.logger.Debug("Provider marked healthy",
		zap.String("provider", ps.providerName),
		zap.Duration("uptime", time.Since(ps.startTime)))

	ProviderHealth.WithLabelValues(ps.providerName).Set(1)
}

// MarkUnhealthy marks the provider as unhealthy
func (ps *ProviderSupervisor) MarkUnhealthy(reason string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.health.Store(int32(HealthUnhealthy))
	ps.logger.Warn("Provider marked unhealthy",
		zap.String("provider", ps.providerName),
		zap.String("reason", reason))

	ProviderHealth.WithLabelValues(ps.providerName).Set(0)
}

// RecordHeartbeatFailure records a failed heartbeat attempt
func (ps *ProviderSupervisor) RecordHeartbeatFailure() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.consecutiveFailures++
	failures := ps.consecutiveFailures

	ps.logger.Warn("Provider heartbeat failed",
		zap.String("provider", ps.providerName),
		zap.Int32("consecutive_failures", failures),
		zap.Int32("max_failures", ps.maxConsecutiveFailures))

	ProviderHeartbeatFailures.WithLabelValues(ps.providerName).Inc()

	if failures >= ps.maxConsecutiveFailures {
		ps.health.Store(int32(HealthUnhealthy))
		ProviderHealth.WithLabelValues(ps.providerName).Set(0)
		return true // Trigger restart
	}

	return false
}

// RecordCrash records a provider process crash
func (ps *ProviderSupervisor) RecordCrash() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.consecutiveCrashes++
	ps.crashCount++
	crashes := ps.consecutiveCrashes
	totalCrashes := ps.crashCount

	ps.logger.Error("Provider process crashed",
		zap.String("provider", ps.providerName),
		zap.Int32("consecutive_crashes", crashes),
		zap.Int32("max_crashes", ps.maxConsecutiveCrashes),
		zap.Int32("total_crashes", totalCrashes))

	ProviderCrashes.WithLabelValues(ps.providerName).Inc()
	ps.health.Store(int32(HealthUnhealthy))
	ProviderHealth.WithLabelValues(ps.providerName).Set(0)

	if crashes >= ps.maxConsecutiveCrashes {
		ps.logger.Error("Provider crash threshold exceeded, disabling provider",
			zap.String("provider", ps.providerName),
			zap.Int32("crashes", crashes))
		return true // Restart threshold exceeded
	}

	return false
}

// GetRestartBackoff returns the backoff delay for the next restart
func (ps *ProviderSupervisor) GetRestartBackoff() time.Duration {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	// Exponential backoff: base * 2^(restartCount-1), capped at maxRestartBackoff
	if ps.restartCount == 0 {
		return 0
	}

	backoff := ps.restartBackoff
	for i := int32(1); i < ps.restartCount; i++ {
		backoff *= 2
		if backoff > ps.maxRestartBackoff {
			backoff = ps.maxRestartBackoff
			break
		}
	}

	// Add jitter (0-10% of backoff)
	jitter := time.Duration((backoff.Nanoseconds() / 10) * (int64(time.Now().UnixNano()) % 10))
	return backoff + jitter
}

// RecordRestart records a restart attempt
func (ps *ProviderSupervisor) RecordRestart(reason string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.restartCount++
	ps.startTime = time.Now()
	ps.consecutiveCrashes = 0
	ps.consecutiveFailures = 0

	ps.logger.Info("Provider process restarting",
		zap.String("provider", ps.providerName),
		zap.String("reason", reason),
		zap.Int32("restart_count", ps.restartCount))

	ProviderRestarts.WithLabelValues(ps.providerName, reason).Inc()
	ps.health.Store(int32(HealthHealthy)) // Assume healthy after restart
	ProviderHealth.WithLabelValues(ps.providerName).Set(1)
}

// ResetRestartCount resets the restart counter on successful operation
func (ps *ProviderSupervisor) ResetRestartCount() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.restartCount > 0 {
		ps.logger.Info("Provider restart counter reset after successful operation",
			zap.String("provider", ps.providerName),
			zap.Int32("was_count", ps.restartCount))
	}

	ps.restartCount = 0
}

// GetHealth returns current health status
func (ps *ProviderSupervisor) GetHealth() ProviderHealthState {
	return ProviderHealthState(ps.health.Load())
}

// GetUptime returns provider process uptime
func (ps *ProviderSupervisor) GetUptime() time.Duration {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	uptime := time.Since(ps.startTime)
	ProviderUptime.WithLabelValues(ps.providerName).Set(uptime.Seconds())
	return uptime
}

// TimeSinceLastHeartbeat returns duration since last successful heartbeat
func (ps *ProviderSupervisor) TimeSinceLastHeartbeat() time.Duration {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	return time.Since(ps.lastHeartbeat)
}

// IsHeartbeatStale checks if heartbeat is stale (no response)
func (ps *ProviderSupervisor) IsHeartbeatStale() bool {
	return ps.TimeSinceLastHeartbeat() > ps.heartbeatTimeout
}

// GetStats returns supervisor statistics
func (ps *ProviderSupervisor) GetStats() map[string]interface{} {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	return map[string]interface{}{
		"provider":             ps.providerName,
		"health":               int(ProviderHealthState(ps.health.Load())),
		"uptime_seconds":       time.Since(ps.startTime).Seconds(),
		"restart_count":        ps.restartCount,
		"crash_count":          ps.crashCount,
		"consecutive_crashes":  ps.consecutiveCrashes,
		"consecutive_failures": ps.consecutiveFailures,
		"time_since_heartbeat": time.Since(ps.lastHeartbeat).Seconds(),
		"heartbeat_stale":      time.Since(ps.lastHeartbeat) > ps.heartbeatTimeout,
	}
}
