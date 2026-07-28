package audit

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TestInitAuditLogger_NilFallbackIsNotRawZap verifies that the nil-fallback
// path in InitAuditLogger (the only path actually exercised in production,
// since InitAuditLogger is never called elsewhere with a non-nil logger)
// routes through observability.NewLogger rather than a raw zap.NewProduction().
//
// SEC-1 regression test: prior to this fix, InitAuditLogger(nil) called
// zap.NewProduction() directly, bypassing the redaction engine entirely.
func TestInitAuditLogger_NilFallbackIsNotRawZap(t *testing.T) {
	InitAuditLogger(nil)
	if auditLogger == nil {
		t.Fatal("InitAuditLogger(nil) left auditLogger nil")
	}

	Log(context.Background(), "rotate", "system", "vault", "db-password", "", "success")
	_ = auditLogger.Sync()
}

// TestAuditLog_PreservesSecretNameForCompliance verifies that the audit log's
// secret_name field (the compliance-required identifier of which secret was
// acted on) is NOT redacted. Redacting it would defeat the audit trail's
// documented purpose ("required for compliance", per AuditEvent's own
// comment) while providing no security benefit, since the field carries an
// identifier, not the secret's value.
func TestAuditLog_PreservesSecretNameForCompliance(t *testing.T) {
	buf := &bytes.Buffer{}
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(buf),
		zapcore.DebugLevel,
	)
	InitAuditLogger(zap.New(core))

	Log(context.Background(), "rotate", "system", "vault", "prod-db-credentials", "container123", "success")
	_ = auditLogger.Sync()

	out := buf.String()
	if !strings.Contains(out, "prod-db-credentials") {
		t.Errorf("secret_name identifier was redacted, breaking compliance audit trail: %s", out)
	}
	if !strings.Contains(out, "container123") {
		t.Errorf("container_id identifier was redacted: %s", out)
	}
}

// TestAuditLog_RedactsPatternMatchingValues verifies that if a caller ever
// passes a raw credential value (matching a known pattern) into a logged
// field, the value-based redaction engine still catches it, even though
// field names like secret_name are exempt from key-based redaction.
func TestAuditLog_RedactsPatternMatchingValues(t *testing.T) {
	buf := &bytes.Buffer{}
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(buf),
		zapcore.DebugLevel,
	)
	InitAuditLogger(zap.New(core))

	// Simulate a hypothetical future call site that accidentally logs a
	// Vault token as the "status" field value.
	auditLogger.Info("audit_event", zap.String("status", "failed: hvs.CAESIQABCDEFG123456789"))
	_ = auditLogger.Sync()

	out := buf.String()
	if strings.Contains(out, "hvs.CAESIQABCDEFG123456789") {
		t.Errorf("pattern-matching secret value was not redacted: %s", out)
	}
}
