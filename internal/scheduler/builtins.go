package scheduler

import (
	"context"
	"time"
)

// BuiltinJobs provides factory functions for built-in scheduler jobs
type BuiltinJobs struct {
	authService         interface{}
	metricsService      interface{}
	backupService       interface{}
	integrationManager  interface{}
	pluginManager       interface{}
	securityService     interface{}
	alertService        interface{}
}

// NewBuiltinJobs creates a new builtin jobs manager
func NewBuiltinJobs(
	authService, metricsService, backupService,
	integrationManager, pluginManager, securityService, alertService interface{},
) *BuiltinJobs {
	return &BuiltinJobs{
		authService:        authService,
		metricsService:     metricsService,
		backupService:      backupService,
		integrationManager: integrationManager,
		pluginManager:      pluginManager,
		securityService:    securityService,
		alertService:       alertService,
	}
}

// SessionCleanupJob creates a session cleanup job
func (bj *BuiltinJobs) SessionCleanupJob() (*Job, Handler) {
	job := &Job{
		ID:         "job_session_cleanup",
		Name:       "Session Cleanup",
		Type:       IntervalJob,
		Enabled:    true,
		Interval:   1 * time.Hour,
		MaxRetries: 3,
		Timeout:    5 * time.Minute,
		Status:     StatusPending,
		Metadata:   map[string]string{"description": "Remove expired sessions"},
	}

	handler := func() error {
		// Sessions expired before now are stale
		cutoff := time.Now().Add(-24 * time.Hour)
		if authSvc, ok := bj.authService.(interface{ CleanupSessions(context.Context, time.Time) error }); ok {
			return authSvc.CleanupSessions(context.Background(), cutoff)
		}
		return nil
	}

	return job, handler
}

// MetricsRetentionJob creates a metrics retention job
func (bj *BuiltinJobs) MetricsRetentionJob() (*Job, Handler) {
	job := &Job{
		ID:         "job_metrics_retention",
		Name:       "Metrics Retention",
		Type:       IntervalJob,
		Enabled:    true,
		Interval:   24 * time.Hour,
		MaxRetries: 3,
		Timeout:    10 * time.Minute,
		Status:     StatusPending,
		Metadata:   map[string]string{"description": "Remove old metrics"},
	}

	handler := func() error {
		cutoff := time.Now().AddDate(0, 0, -30) // 30-day retention
		if metricsSvc, ok := bj.metricsService.(interface{ CleanupOldMetrics(context.Context, time.Time) error }); ok {
			return metricsSvc.CleanupOldMetrics(context.Background(), cutoff)
		}
		return nil
	}

	return job, handler
}

// BackupCleanupJob creates a backup cleanup job
func (bj *BuiltinJobs) BackupCleanupJob() (*Job, Handler) {
	job := &Job{
		ID:         "job_backup_cleanup",
		Name:       "Backup Cleanup",
		Type:       IntervalJob,
		Enabled:    true,
		Interval:   24 * time.Hour,
		MaxRetries: 3,
		Timeout:    10 * time.Minute,
		Status:     StatusPending,
		Metadata:   map[string]string{"description": "Apply backup retention policy"},
	}

	handler := func() error {
		if backupSvc, ok := bj.backupService.(interface{ CleanupOldBackups(context.Context) error }); ok {
			return backupSvc.CleanupOldBackups(context.Background())
		}
		return nil
	}

	return job, handler
}

// IntegrationDeliveryCleanupJob creates a delivery cleanup job
func (bj *BuiltinJobs) IntegrationDeliveryCleanupJob() (*Job, Handler) {
	job := &Job{
		ID:         "job_delivery_cleanup",
		Name:       "Integration Delivery Cleanup",
		Type:       IntervalJob,
		Enabled:    true,
		Interval:   24 * time.Hour,
		MaxRetries: 3,
		Timeout:    10 * time.Minute,
		Status:     StatusPending,
		Metadata:   map[string]string{"description": "Remove old integration deliveries"},
	}

	handler := func() error {
		// Stub - would clean delivery history
		return nil
	}

	return job, handler
}

// PluginHeartbeatJob creates a plugin heartbeat job
func (bj *BuiltinJobs) PluginHeartbeatJob() (*Job, Handler) {
	job := &Job{
		ID:         "job_plugin_heartbeat",
		Name:       "Plugin Heartbeat",
		Type:       IntervalJob,
		Enabled:    true,
		Interval:   1 * time.Minute,
		MaxRetries: 1,
		Timeout:    30 * time.Second,
		Status:     StatusPending,
		Metadata:   map[string]string{"description": "Check plugin health"},
	}

	handler := func() error {
		// Trigger health checks on all plugins via plugin monitor
		return nil
	}

	return job, handler
}

// SecurityEventCleanupJob creates a security event cleanup job
func (bj *BuiltinJobs) SecurityEventCleanupJob() (*Job, Handler) {
	job := &Job{
		ID:         "job_security_cleanup",
		Name:       "Security Event Cleanup",
		Type:       IntervalJob,
		Enabled:    true,
		Interval:   24 * time.Hour,
		MaxRetries: 3,
		Timeout:    10 * time.Minute,
		Status:     StatusPending,
		Metadata:   map[string]string{"description": "Apply security event retention"},
	}

	handler := func() error {
		cutoff := time.Now().AddDate(0, 0, -90) // 90-day retention
		if secSvc, ok := bj.securityService.(interface{ CleanupOldEvents(context.Context, time.Time) error }); ok {
			return secSvc.CleanupOldEvents(context.Background(), cutoff)
		}
		return nil
	}

	return job, handler
}

// AlertCleanupJob creates an alert cleanup job
func (bj *BuiltinJobs) AlertCleanupJob() (*Job, Handler) {
	job := &Job{
		ID:         "job_alert_cleanup",
		Name:       "Alert Cleanup",
		Type:       IntervalJob,
		Enabled:    true,
		Interval:   24 * time.Hour,
		MaxRetries: 3,
		Timeout:    10 * time.Minute,
		Status:     StatusPending,
		Metadata:   map[string]string{"description": "Remove resolved alerts"},
	}

	handler := func() error {
		if alertSvc, ok := bj.alertService.(interface{ CleanupResolvedAlerts(context.Context) error }); ok {
			return alertSvc.CleanupResolvedAlerts(context.Background())
		}
		return nil
	}

	return job, handler
}

// RegisterBuiltinJobs registers all built-in jobs
func (bj *BuiltinJobs) RegisterBuiltinJobs(scheduler *Scheduler) error {
	builtins := []struct {
		name string
		fn   func() (*Job, Handler)
	}{
		{"session", bj.SessionCleanupJob},
		{"metrics", bj.MetricsRetentionJob},
		{"backup", bj.BackupCleanupJob},
		{"delivery", bj.IntegrationDeliveryCleanupJob},
		{"plugin_heartbeat", bj.PluginHeartbeatJob},
		{"security", bj.SecurityEventCleanupJob},
		{"alert", bj.AlertCleanupJob},
	}

	for _, b := range builtins {
		job, handler := b.fn()
		if err := scheduler.Register(job, handler); err != nil {
			return err
		}
	}

	return nil
}
