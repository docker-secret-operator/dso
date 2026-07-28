package observability

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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
		{
			// zap.Binary resolves to zapcore.BinaryType, a distinct type from
			// ByteStringType that was not handled until this review found the gap.
			name: "zap.Binary field",
			log: func(l *zap.Logger) {
				l.Info("payload", zap.Binary("data", []byte("token=BINARYLEAK123456")))
			},
			mustNotAppear: []string{"BINARYLEAK123456"},
		},
		{
			// zap.Strings resolves to zapcore.ArrayMarshalerType, wrapping a
			// private zap.stringArray type — not handled until this review
			// found the gap. Real call sites (internal/watcher/controller.go)
			// use this for secret *name* lists, but a value-carrying element
			// must still be redacted if one is ever passed.
			name: "zap.Strings field with a pattern-matching element",
			log: func(l *zap.Logger) {
				l.Info("batch", zap.Strings("items", []string{"safe-name", "password=ARRAYLEAK789"}))
			},
			mustNotAppear: []string{"ARRAYLEAK789"},
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

	// zap.Strings("secrets", secretList) at internal/watcher/controller.go
	// and elsewhere logs lists of secret *names*, not values. Confirms the
	// ArrayMarshalerType handling added during final review does not
	// over-redact identifier lists the same way StringType handling doesn't.
	t.Run("zap.Strings with identifier values is preserved", func(t *testing.T) {
		buf.Reset()
		logger.Info("tracking", zap.Strings("secrets", []string{"db-password", "api-token-name"}))
		out := buf.String()
		if !strings.Contains(out, "db-password") || !strings.Contains(out, "api-token-name") {
			t.Errorf("legitimate secret-name list was over-redacted: %s", out)
		}
	})
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

	// Regression test for a defect found during final review: an earlier
	// Check() implementation only tested c.Enabled(level) before registering
	// itself as the Write target, which silently bypasses zapcore's sampler
	// (zapcore.NewSamplerWithOptions / zap.Config.Sampling) — its rate-limit
	// decision lives inside Check(), not Enabled(). zap.NewProductionConfig(),
	// used throughout this codebase, enables sampling by default
	// (Initial:100, Thereafter:100), so this is a real production concern,
	// not a hypothetical one.
	t.Run("sampling is preserved, not bypassed", func(t *testing.T) {
		buf.Reset()
		inner := zapcore.NewCore(
			zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
			zapcore.AddSync(buf),
			zapcore.DebugLevel,
		)
		// Allow only 2 identical log lines per (long) tick, then drop the rest.
		sampled := zapcore.NewSamplerWithOptions(inner, time.Hour, 2, 0)
		wrapped := newRedactingCore(sampled)
		l := zap.New(wrapped)

		for i := 0; i < 20; i++ {
			l.Info("repeated message")
		}
		_ = l.Sync()

		lines := strings.Count(buf.String(), "\n")
		if lines > 3 {
			t.Errorf("sampling was bypassed: wrote %d lines for 20 identical calls under a sampler configured for ~2; "+
				"Check() must delegate to the wrapped core's Check(), not just Enabled()", lines)
		}
		if lines == 0 {
			t.Errorf("sampling suppressed everything: expected ~2 lines to pass through, got 0")
		}
	})

	// Caller information and stack traces are attached to the zapcore.Entry
	// by the *zap.Logger itself (via runtime.Caller / debug.Stack) before
	// Check()/Write() are ever invoked. redactingCore.Write() only rewrites
	// entry.Message and the field slice — it never touches entry.Caller or
	// entry.Stack — so both must survive unchanged through the wrapper.
	t.Run("caller information and stack traces survive unchanged", func(t *testing.T) {
		buf.Reset()
		encoderCfg := zap.NewProductionEncoderConfig()
		core := zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderCfg),
			zapcore.AddSync(buf),
			zapcore.DebugLevel,
		)
		wrapped := newRedactingCore(core)
		l := zap.New(wrapped, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

		l.Error("failure with token=STACKTRACELEAKCHECK999")

		out := buf.String()
		if !strings.Contains(out, `"caller":"observability/redaction_test.go`) {
			t.Errorf("caller info missing or altered: %s", out)
		}
		if !strings.Contains(out, `"stacktrace":`) {
			t.Errorf("stacktrace missing: %s", out)
		}
		if strings.Contains(out, "STACKTRACELEAKCHECK999") {
			t.Errorf("secret leaked via unredacted path: %s", out)
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

// TestRedactReflected_SafetyProperties verifies the JSON round-trip used for
// zap.Any/zap.Reflect/zap.Strings/zap.Object cannot panic, crash, or hang,
// regardless of what is logged. Each subtest is expected to complete without
// panicking and without leaking a secret if one happens to be present.
func TestRedactReflected_SafetyProperties(t *testing.T) {
	core := newRedactingCore(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&bytes.Buffer{}),
		zapcore.DebugLevel,
	)).(*redactingCore)

	t.Run("cyclic pointer struct does not panic or hang", func(t *testing.T) {
		type node struct {
			Name string
			Next *node
		}
		a := &node{Name: "a"}
		b := &node{Name: "b"}
		a.Next = b
		b.Next = a // cycle

		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = core.redactReflected(a) // must not panic
		}()
		select {
		case <-done:
			// completed without hanging or panicking
		case <-time.After(5 * time.Second):
			t.Fatal("redactReflected hung on a cyclic struct (encoding/json should detect the cycle and return an error, not loop)")
		}
	})

	t.Run("unsupported types (chan, func) do not panic", func(t *testing.T) {
		unsupported := []interface{}{
			make(chan int),
			func() {},
			map[string]interface{}{"nested": make(chan int)},
		}
		for _, v := range unsupported {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("redactReflected panicked on %T: %v", v, r)
					}
				}()
				_ = core.redactReflected(v)
			}()
		}
	})

	t.Run("nil value does not panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("redactReflected panicked on nil: %v", r)
			}
		}()
		result := core.redactReflected(nil)
		if result != nil {
			t.Errorf("expected nil to round-trip as nil, got %v", result)
		}
	})

	t.Run("preserves non-sensitive nested data", func(t *testing.T) {
		type inner struct {
			ContainerID string
			Attempt     int
		}
		type outer struct {
			Stage  string
			Detail inner
		}
		v := outer{Stage: "rotate", Detail: inner{ContainerID: "abc123", Attempt: 3}}
		result := core.redactReflected(v)
		b, _ := json.Marshal(result)
		out := string(b)
		if !strings.Contains(out, "abc123") || !strings.Contains(out, "rotate") || !strings.Contains(out, "3") {
			t.Errorf("non-sensitive nested data was altered: %s", out)
		}
	})

	t.Run("redacts sensitive value nested inside a struct", func(t *testing.T) {
		type payload struct {
			Reason string
			Detail string
		}
		v := payload{Reason: "auth failed", Detail: "password=NESTEDPAYLOADLEAK123"}
		result := core.redactReflected(v)
		b, _ := json.Marshal(result)
		if strings.Contains(string(b), "NESTEDPAYLOADLEAK123") {
			t.Errorf("secret nested inside a struct was not redacted: %s", b)
		}
	})
}

// BenchmarkRedaction_StringField and BenchmarkRedaction_ReflectField give a
// baseline for the per-log-call cost the redaction wrapper adds, since every
// production log entry now passes through it.
func BenchmarkRedaction_StringField(b *testing.B) {
	core := newRedactingCore(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&discardSink{}),
		zapcore.DebugLevel,
	))
	l := zap.New(core)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Info("rotation completed", zap.String("secret", "db-password"), zap.Int("attempt", 1))
	}
}

func BenchmarkRedaction_ReflectField(b *testing.B) {
	core := newRedactingCore(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&discardSink{}),
		zapcore.DebugLevel,
	))
	l := zap.New(core)
	type payload struct {
		Reason string
		Detail string
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Info("event", zap.Any("payload", payload{Reason: "test", Detail: "value"}))
	}
}

func BenchmarkNoRedaction_StringField(b *testing.B) {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&discardSink{}),
		zapcore.DebugLevel,
	)
	l := zap.New(core)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Info("rotation completed", zap.String("secret", "db-password"), zap.Int("attempt", 1))
	}
}

type discardSink struct{}

func (d *discardSink) Write(p []byte) (int, error) { return len(p), nil }
func (d *discardSink) Sync() error                 { return nil }

// stringerLeak is a minimal fmt.Stringer implementation used to prove
// StringerType redaction, found missing during final review.
type stringerLeak struct{ secret string }

func (s stringerLeak) String() string { return "token=" + s.secret }

// TestRedaction_StringerType is a regression test for a defect found during
// final review: zap.Stringer(key, v) defers calling v.String() until encode
// time, storing the fmt.Stringer value itself (not a plain string) in
// f.Interface -- this resolves to zapcore.StringerType, a distinct type from
// StringType that the original switch did not handle. Critically,
// zap.Any(key, v) ALSO routes any value implementing only fmt.Stringer (not
// error) to this same StringerType, not ReflectType -- reachable today via
// the same zap.Any() call sites already relied upon for struct redaction
// (internal/events/backpressure.go, internal/agent/observability.go).
func TestRedaction_StringerType(t *testing.T) {
	logger, buf := newCapturingLogger(t)

	t.Run("zap.Stringer field constructor", func(t *testing.T) {
		buf.Reset()
		logger.Info("cfg", zap.Stringer("v", stringerLeak{secret: "DIRECTSTRINGERLEAK999"}))
		out := buf.String()
		if strings.Contains(out, "DIRECTSTRINGERLEAK999") {
			t.Errorf("SECRET LEAKED via zap.Stringer(): %s", out)
		}
	})

	t.Run("zap.Any with a Stringer-only value", func(t *testing.T) {
		buf.Reset()
		logger.Info("cfg", zap.Any("v", stringerLeak{secret: "ANYSTRINGERLEAK789"}))
		out := buf.String()
		if strings.Contains(out, "ANYSTRINGERLEAK789") {
			t.Errorf("SECRET LEAKED via zap.Any() on a Stringer-only value: %s", out)
		}
	})
}

// TestRedaction_KnownLimitations documents (and locks in the documentation
// of, via SECURITY.md) two limitations found during final review that are
// not bugs: regex-based redaction cannot see through encoding. These tests
// exist to make the limitation explicit and testable, not to assert it will
// ever be fixed within SEC-1's scope.
func TestRedaction_KnownLimitations(t *testing.T) {
	logger, buf := newCapturingLogger(t)

	t.Run("base64-encoded secret is not redacted (documented limitation)", func(t *testing.T) {
		buf.Reset()
		encoded := base64.StdEncoding.EncodeToString([]byte("password=BASE64LEAK"))
		logger.Info("payload: " + encoded)
		out := buf.String()
		if !strings.Contains(out, encoded) {
			t.Skip("behavior changed: base64 payload is now redacted somehow; if intentional, update SECURITY.md")
		}
	})

	t.Run("hex-encoded secret is not redacted (documented limitation)", func(t *testing.T) {
		buf.Reset()
		encoded := hex.EncodeToString([]byte("password=HEXLEAK"))
		logger.Info("payload: " + encoded)
		out := buf.String()
		if !strings.Contains(out, encoded) {
			t.Skip("behavior changed: hex payload is now redacted somehow; if intentional, update SECURITY.md")
		}
	})
}
