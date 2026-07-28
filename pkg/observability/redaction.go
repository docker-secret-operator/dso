package observability

import (
	"encoding/json"
	"errors"
	"fmt"

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

// Check delegates the logging decision to the wrapped core using a throwaway
// CheckedEntry, then — only if the wrapped core would log the entry —
// registers THIS core (not the wrapped one) on the real ce, so Write() still
// redacts. A naive `if c.Enabled(level) { ce.AddCore(entry, c) }` (checking
// only the level) would silently bypass zap's sampling: zapcore's sampler
// implements its rate-limiting decision inside Check(), not Enabled(), so
// skipping the delegation call disables sampling entirely for any sampled
// core wrapped by this decorator. Verified: zap.NewProductionConfig()
// (used throughout this codebase) enables sampling by default
// (Initial:100, Thereafter:100), so this matters in production.
func (c *redactingCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if downstream := c.Core.Check(entry, nil); downstream != nil {
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

// redactFields redacts the value of every field that can carry a raw string,
// error, or arbitrary structured value: zap.String, zap.ByteString,
// zap.Binary, zap.Error, zap.Stringer, zap.Any, zap.Reflect, zap.Strings/
// zap.Ints/etc. (ArrayMarshalerType), and zap.Object (ObjectMarshalerType).
// Field types that cannot carry credential text (Int, Bool, Duration, Time,
// Namespace, Skip, etc.) pass through unchanged via the default case.
// zap.Inline (InlineMarshalerType) is a known, documented gap: see
// SECURITY.md's residual-risk section -- it shares ObjectMarshaler's
// interface but converting it to ReflectType would break its field-
// flattening encode behavior, and it has zero production call sites today.
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

	case zapcore.ByteStringType, zapcore.BinaryType:
		// zap.ByteString and zap.Binary both carry a plain []byte in
		// f.Interface (verified: zap.Binary's field.Interface is []uint8,
		// identical representation to ByteStringType), so both are handled
		// identically via direct pattern matching on the byte content.
		if b, ok := f.Interface.([]byte); ok {
			f.Interface = []byte(c.redactor.RedactString(string(b)))
		}
		return f

	case zapcore.ArrayMarshalerType, zapcore.ObjectMarshalerType:
		// zap.Strings/zap.Ints/etc. (ArrayMarshalerType) and zap.Object
		// (ObjectMarshalerType) wrap values in zap's own private marshaler
		// types (e.g. zap.stringArray), not plain slices/structs, so they
		// can't be pattern-matched directly. Verified that json.Marshal
		// correctly serializes these wrapper types via reflection (they are
		// just named slice/struct types underneath), and that zap's JSON
		// encoder renders ReflectType fields via encoding/json regardless of
		// the concrete type — so converting to ReflectType with the
		// redacted, round-tripped value re-encodes correctly.
		// Real production call site: zap.Strings("secrets", secretList) in
		// internal/watcher/controller.go and others logs lists of secret
		// *names* (identifiers, safe by this codebase's convention), but
		// this closes the type for any future caller logging real values.
		return zapcore.Field{Key: f.Key, Type: zapcore.ReflectType, Interface: c.redactReflected(f.Interface)}

	case zapcore.ErrorType:
		if err, ok := f.Interface.(error); ok {
			f.Interface = errors.New(c.redactor.RedactError(err))
		}
		return f

	case zapcore.StringerType:
		// zap.Stringer(key, v) defers calling v.String() until encode time,
		// storing the fmt.Stringer value itself in f.Interface (verified: it
		// does NOT resolve to StringType immediately). Found during final
		// review that zap.Any(key, v) ALSO routes any value implementing only
		// fmt.Stringer (not error) to this same StringerType, not ReflectType
		// -- reachable today via the same zap.Any() call sites already
		// covered for structs (internal/events/backpressure.go,
		// internal/agent/observability.go), so this is not a hypothetical
		// gap. Handled the same way as ErrorType: call the interface's
		// defining method, redact the resulting string, re-wrap as a plain
		// StringType field.
		if s, ok := f.Interface.(fmt.Stringer); ok {
			return zapcore.Field{Key: f.Key, Type: zapcore.StringType, String: c.redactor.RedactString(s.String())}
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
