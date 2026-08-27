package audit

import (
	"context"
	"time"

	"github.com/docker-secret-operator/dso/pkg/observability"
	"go.uber.org/zap"
)

// AuditEvent represents a single structured audit log entry required for compliance
type AuditEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Level       string    `json:"level"`
	Action      string    `json:"event"`
	User        string    `json:"user"`
	Provider    string    `json:"provider"`
	SecretName  string    `json:"secret_name"`
	ContainerID string    `json:"container_id,omitempty"`
	Status      string    `json:"status"`
}

// Global audit logger initialized based on standard observability settings
var auditLogger *zap.Logger

func InitAuditLogger(l *zap.Logger) {
	if l == nil {
		// SEC-1: route through observability.NewLogger so audit output passes
		// through the shared redaction core instead of a raw zap logger.
		var err error
		l, err = observability.NewLogger("info", "json", true)
		if err != nil {
			l = zap.NewNop()
		}
	} else {
		// SEC-1: InitAuditLogger is exported, so a future caller could pass an
		// externally-constructed logger that never went through
		// observability.NewLogger. EnsureRedacted guarantees the same
		// no-secrets-in-logs guarantee applies regardless of how l was built.
		l = observability.EnsureRedacted(l)
	}
	auditLogger = l.Named("audit")
}

// Log records a compliant JSON event structure to standard out
func Log(_ context.Context, action string, user string, provider string, secretName string, containerID string, status string) {
	if auditLogger == nil {
		InitAuditLogger(nil)
	}

	fields := []zap.Field{
		zap.String("event", action),
		zap.String("user", user),
		zap.String("provider", provider),
		zap.String("secret_name", secretName),
		zap.String("status", status),
	}

	if containerID != "" {
		fields = append(fields, zap.String("container_id", containerID))
	}

	auditLogger.Info("audit_event", fields...)

	// Project "secret_fetch" activity into the queryable EventStore
	// (internal/server/eventstore.go, fed via observability.EventStream) --
	// previously this activity was only visible in the zap log stream, not
	// through /api/audit or /api/events. Deliberately excludes the "rotate"
	// action: rotation outcomes already flow into EventStream via
	// internal/agent.TriggerEngine.emitEvent, which is the single
	// authoritative completion point for every rotation strategy
	// (restart/rolling/signal). internal/watcher/controller.go's per-target
	// audit.Log(ctx, "rotate", ...) calls only fire inside the rolling-
	// strategy branch, so routing "rotate" here too would double-count
	// rolling-strategy rotations. Metadata only -- action/provider/secret
	// name/container id/status, never a secret value.
	if action != "secret_fetch" {
		return
	}
	normalizedStatus := status
	if normalizedStatus == "failed" {
		// Normalize to the "success"/"failure" convention every other
		// EventStream producer already uses (LogInjectionEvent,
		// TriggerEngine.emitEvent), so the existing frontend's severity
		// badge/filter logic (which checks ev.status === 'failure') applies
		// consistently instead of silently falling into its "unknown"
		// (warning) bucket for fetch failures specifically.
		normalizedStatus = "failure"
	}
	streamEvent := map[string]interface{}{
		"timestamp":  time.Now().Format(time.RFC3339),
		"secret":     secretName,
		"event_type": action,
		"status":     normalizedStatus,
	}
	if provider != "" {
		streamEvent["provider"] = provider
	}
	if containerID != "" {
		streamEvent["container"] = containerID
	}
	select {
	case observability.EventStream <- streamEvent:
	default:
	}
}
