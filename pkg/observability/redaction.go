package observability

import (
	"encoding/json"
	"errors"

	"github.com/docker-secret-operator/dso/pkg/security"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// redactingCore wraps a zapcore.Core and redacts sensitive values from the
// log entry message and every field before delegating to the wrapped core.
// It is a transparent decorator: level filtering, sampling, With(), Named(),
// Check(), and Sync() all pass through to the wrapped core unchanged. Only
// the content of Write() is transformed.
//
// SEC-1: this closes the gap where pkg/security's redaction engine existed
// but was never invoked by any production logging path.
type redactingCore struct {
	zapcore.Core
	redactor *security.RedactionPatterns
}

// newRedactingCore constructs a redactingCore wrapping the given core.
func newRedactingCore(core zapcore.Core) zapcore.Core {
	return &redactingCore{
		Core:     core,
		redactor: security.NewRedactionPatterns(),
	}
}

// EnsureRedacted returns a logger that writes through the redaction core,
// wrapping l if it does not already use one. This lets any package that
// accepts an externally-constructed *zap.Logger (rather than building one
// via NewLogger itself) guarantee the same SEC-1 guarantee applies, even to
// loggers built outside this package.
func EnsureRedacted(l *zap.Logger) *zap.Logger {
	if l == nil {
		return l
	}
	return l.WithOptions(redactingWrapOption)
}

// With returns a new core with additional fields, redacting them first so
// that secrets attached via logger.With(...) cannot bypass redaction.
func (c *redactingCore) With(fields []zapcore.Field) zapcore.Core {
	return &redactingCore{
		Core:     c.Core.With(c.redactFields(fields)),
		redactor: c.redactor,
	}
}

// Check delegates to the wrapped core's level-enabled logic, then registers
// this core (not the raw wrapped core) as the one to call Write on, so
// redaction is not skipped by zap's fast-path optimizations.
func (c *redactingCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

// Write redacts the entry message and all fields, then delegates to the
// wrapped core. This is the single choke point every log call passes
// through, regardless of which zap.Field constructor was used.
func (c *redactingCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	entry.Message = c.redactor.RedactString(entry.Message)
	return c.Core.Write(entry, c.redactFields(fields))
}

// redactFields redacts the value of every field that can carry a raw string
// or error, covering the common zap field constructors used across the
// codebase: zap.String, zap.Error, zap.Any(error), zap.Reflect, and
// zap.ByteString. Non-string, non-error field types (numbers, bools,
// durations, timestamps) are passed through unchanged since they cannot
// carry secret material in this codebase's usage.
func (c *redactingCore) redactFields(fields []zapcore.Field) []zapcore.Field {
	out := make([]zapcore.Field, len(fields))
	for i, f := range fields {
		out[i] = c.redactField(f)
	}
	return out
}

func (c *redactingCore) redactField(f zapcore.Field) zapcore.Field {
	// NOTE: field-key-based redaction (e.g. treating any field named "secret"
	// as sensitive regardless of content) was deliberately evaluated and
	// rejected here. This codebase's own convention uses zap.String("secret",
	// secretName) and zap.String("secret_name", secretName) at ~30 call sites
	// across internal/agent, internal/server, and internal/injector to log
	// the *identifier* of a secret (which one was rotated/fetched), not its
	// value. Key-substring matching on security.ShouldLogField would redact
	// all of these, destroying operational and audit visibility, to guard
	// against a threat with zero evidence in this codebase (no call site logs
	// a raw credential value through a sensitively-named zap.String field;
	// verified by repository-wide grep). The value-pattern matching below
	// already covers the real, evidenced threat: provider SDK errors that
	// embed tokens/credentials in their message text.
	switch f.Type {
	case zapcore.StringType:
		f.String = c.redactor.RedactString(f.String)
		return f

	case zapcore.ByteStringType:
		if b, ok := f.Interface.([]byte); ok {
			f.Interface = []byte(c.redactor.RedactString(string(b)))
		}
		return f

	case zapcore.ErrorType:
		if err, ok := f.Interface.(error); ok {
			f.Interface = errors.New(c.redactor.RedactError(err))
		}
		return f

	case zapcore.ReflectType:
		// zap.Any(...) with an error value resolves to ErrorType above already.
		// zap.Any(...)/zap.Reflect(...) with an arbitrary struct resolves here.
		// Two real call sites in this codebase use this path: a recovered
		// panic value (internal/events/backpressure.go) and dynamic rotation
		// metadata (internal/agent/observability.go) — either could carry a
		// struct containing a credential in a future call site. Since regex
		// patterns can't be applied to a Go struct directly, the value is
		// marshaled to JSON, redacted as text, and unmarshaled back so the
		// encoder still emits structured output rather than a quoted string.
		if err, ok := f.Interface.(error); ok {
			return zapcore.Field{Key: f.Key, Type: zapcore.ErrorType, Interface: errors.New(c.redactor.RedactError(err))}
		}
		return zapcore.Field{Key: f.Key, Type: zapcore.ReflectType, Interface: c.redactReflected(f.Interface)}

	default:
		return f
	}
}

// redactReflected redacts sensitive substrings from an arbitrary value logged
// via zap.Any/zap.Reflect by round-tripping it through JSON. If marshaling
// fails (e.g. the value contains a channel or func), the original value is
// returned unchanged rather than dropped, since failing to marshal is not
// itself a redaction bypass — zap's own encoder would hit the same error.
func (c *redactingCore) redactReflected(v interface{}) interface{} {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	redacted := c.redactor.RedactString(string(raw))
	var out interface{}
	if err := json.Unmarshal([]byte(redacted), &out); err != nil {
		// Redaction may have broken JSON structure (e.g. redacting inside a
		// quoted string is safe, but a pattern spanning a structural
		// character would not be). Fall back to the redacted text itself so
		// the secret still does not appear, even if formatting degrades.
		return redacted
	}
	return out
}
