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
}
