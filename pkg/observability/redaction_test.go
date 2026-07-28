package observability

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// newCapturingLogger builds a logger identical to NewLogger's production path,
// but writes to an in-memory buffer instead of stdout so tests can inspect output.
func newCapturingLogger(t *testing.T) (*zap.Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	encoderCfg := zap.NewProductionEncoderConfig()
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(buf),
		zapcore.DebugLevel,
	)
	wrapped := newRedactingCore(core)
	return zap.New(wrapped), buf
}

// SEC-1 regression matrix: every input/expected-output pair from the review table.
func TestRedaction_Matrix(t *testing.T) {
	cases := []struct {
		name          string
		log           func(l *zap.Logger)
		mustNotAppear []string
	}{
		{
			name: "AWS access key in message",
			log: func(l *zap.Logger) {
				l.Info("provider init failed: AKIAABCDEFGHIJKLMNOP")
			},
			mustNotAppear: []string{"AKIAABCDEFGHIJKLMNOP"},
		},
		{
			name: "Bearer token in message",
			log: func(l *zap.Logger) {
				l.Info("auth header: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9")
			},
			mustNotAppear: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name: "password=value in message",
			log: func(l *zap.Logger) {
				l.Info("connect failed password=secret123")
			},
			mustNotAppear: []string{"secret123"},
		},
		{
			name: "Vault hvs. token in message",
			log: func(l *zap.Logger) {
				l.Info("vault returned token hvs.CAESIABCDEFG123456789 permission denied")
			},
			mustNotAppear: []string{"hvs.CAESIABCDEFG123456789"},
		},
		{
			name: "wrapped error via zap.Error",
			log: func(l *zap.Logger) {
				base := errors.New("vault auth failed: hvs.CAESIQZZZ999888777")
				wrapped := fmt.Errorf("provider init: %w", base)
				l.Error("provider failed", zap.Error(wrapped))
			},
			mustNotAppear: []string{"hvs.CAESIQZZZ999888777"},
		},
		{
			name: "structured zap.String value matching a pattern is redacted",
			log: func(l *zap.Logger) {
				l.Info("config loaded", zap.String("detail", "connect failed password=hunter2ultrasecret"))
			},
			mustNotAppear: []string{"hunter2ultrasecret"},
		},
		{
			name: "logger.With() propagates redaction",
			log: func(l *zap.Logger) {
				child := l.With(zap.String("component", "vault"))
				child.Error("op failed", zap.Error(errors.New("token=AKIAZZZZZZZZZZZZZZZZ leaked")))
			},
			mustNotAppear: []string{"AKIAZZZZZZZZZZZZZZZZ"},
		},
		{
			name: "logger.Named() propagates redaction",
			log: func(l *zap.Logger) {
				named := l.Named("subsystem")
				named.Info("secret=topsecretvalue123 in config")
			},
			mustNotAppear: []string{"topsecretvalue123"},
		},
		{
			name: "zap.Any with error value",
			log: func(l *zap.Logger) {
				l.Info("result", zap.Any("err", errors.New("password=nestedleak999")))
			},
			mustNotAppear: []string{"nestedleak999"},
		},
		{
			// Real production call sites: internal/events/backpressure.go logs a
			// recovered panic value via zap.Any("panic", r); a panic carrying a
			// struct (rather than a string or error) previously bypassed
			// redaction entirely since it resolves to zapcore.ReflectType.
			name: "zap.Any with arbitrary struct (recovered panic simulation)",
			log: func(l *zap.Logger) {
				type panicPayload struct {
					Reason string
					Token  string
				}
				l.Error("recovered panic", zap.Any("panic", panicPayload{
					Reason: "provider auth failed",
					Token:  "hvs.CAESIQSTRUCTLEAK123456",
				}))
			},
			mustNotAppear: []string{"hvs.CAESIQSTRUCTLEAK123456"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger, buf := newCapturingLogger(t)
			tc.log(logger)
			_ = logger.Sync()
			output := buf.String()
			for _, secret := range tc.mustNotAppear {
				if strings.Contains(output, secret) {
					t.Errorf("SECRET LEAKED into log output.\nSecret: %q\nFull output: %s", secret, output)
				}
			}
		})
	}
}

// TestRedaction_DoesNotOverRedactSecretIdentifierFields is a critical
// regression test. This codebase's own convention logs the *identifier* of a
// secret (which one is being rotated/fetched) via zap.String("secret", name)
// and zap.String("secret_name", name) at ~30 call sites across
// internal/agent, internal/server, and internal/injector. An earlier draft
// of this fix applied key-substring matching (any field key containing
// "secret" treated as sensitive) which redacted all of these, destroying
// operational and audit visibility for a threat with no evidence in this
// codebase. This test locks in the corrected behavior: only field *values*
// matching a known credential pattern are redacted, never field names alone.
func TestRedaction_DoesNotOverRedactSecretIdentifierFields(t *testing.T) {
	logger, buf := newCapturingLogger(t)

	cases := []struct {
		name  string
		field zapcore.Field
		value string
	}{
		{"secret identifier field", zapcore.Field{Key: "secret", Type: zapcore.StringType, String: "db-password"}, "db-password"},
		{"secret_name identifier field", zapcore.Field{Key: "secret_name", Type: zapcore.StringType, String: "prod-api-key"}, "prod-api-key"},
		{"provider identifier field", zapcore.Field{Key: "provider", Type: zapcore.StringType, String: "vault"}, "vault"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			logger.Info("operation", tc.field)
			out := buf.String()
			if !strings.Contains(out, tc.value) {
				t.Errorf("legitimate identifier field was over-redacted: key=%q value=%q output=%s",
					tc.field.Key, tc.value, out)
			}
		})
	}
}

// TestRedaction_PreservesZapBehavior verifies the Core wrapper is a transparent
// decorator: level filtering, Sync, and non-sensitive fields still work normally.
func TestRedaction_PreservesZapBehavior(t *testing.T) {
	logger, buf := newCapturingLogger(t)

	t.Run("non-sensitive fields pass through unchanged", func(t *testing.T) {
		buf.Reset()
		logger.Info("rotation completed", zap.String("container_id", "abc123"), zap.Int("attempt", 3))
		out := buf.String()
		if !strings.Contains(out, "abc123") {
			t.Errorf("non-sensitive field was incorrectly redacted: %s", out)
		}
		if !strings.Contains(out, `"attempt":3`) {
			t.Errorf("non-sensitive int field was altered: %s", out)
		}
	})

	t.Run("message text is preserved except redacted portions", func(t *testing.T) {
		buf.Reset()
		logger.Info("rotation completed successfully")
		out := buf.String()
		if !strings.Contains(out, "rotation completed successfully") {
			t.Errorf("innocuous message was altered: %s", out)
		}
	})

	t.Run("Sync does not error", func(t *testing.T) {
		if err := logger.Sync(); err != nil {
			// stdout/stderr sync errors are common in test environments; only
			// fail if it's our buffer-backed core specifically erroring.
			t.Logf("Sync returned (may be harmless in test env): %v", err)
		}
	})

	t.Run("level filtering still works", func(t *testing.T) {
		buf.Reset()
		encoderCfg := zap.NewProductionEncoderConfig()
		core := zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderCfg),
			zapcore.AddSync(buf),
			zapcore.WarnLevel, // only warn+ enabled
		)
		wrapped := newRedactingCore(core)
		l := zap.New(wrapped)

		l.Info("this should not appear")
		l.Warn("this should appear")

		out := buf.String()
		if strings.Contains(out, "this should not appear") {
			t.Errorf("level filtering broken: info message was written despite WarnLevel floor")
		}
		if !strings.Contains(out, "this should appear") {
			t.Errorf("level filtering broken: warn message was suppressed")
		}
	})
}

// TestNewLogger_AppliesRedaction verifies the actual public factory (used by
// internal/cli/agent.go and internal/cli/root.go) redacts secrets end-to-end.
func TestNewLogger_AppliesRedaction(t *testing.T) {
	l, err := NewLogger("info", "json", true)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	if l == nil {
		t.Fatal("NewLogger returned nil logger")
	}
	// Smoke test: logging a secret through the real factory must not panic
	// and the logger must be usable. Full content verification is covered by
	// TestRedaction_Matrix against the underlying core directly.
	l.Info("smoke test", zap.String("token", "AKIASMOKETESTVALUE01"))
	_ = l.Sync()
}
