package observability

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is a global logger instance for convenience, though passing logger by context is preferred.
var Logger *zap.Logger

// redactingWrapOption wraps the base core built by zap in a redactingCore so
// that every logger produced by this package redacts sensitive values before
// they are written. See redaction.go (SEC-1).
var redactingWrapOption = zap.WrapCore(func(core zapcore.Core) zapcore.Core {
	return newRedactingCore(core)
})

func init() {
	Logger, _ = zap.NewProduction(redactingWrapOption)
}

// NewLogger creates a new configured zap logger with level and format (json/text).
// All output passes through a redaction core (SEC-1) so secrets and credentials
// are not written to logs even when errors from provider SDKs embed them.
func NewLogger(level string, format string, isProduction bool) (*zap.Logger, error) {
	var cfg zap.Config
	if isProduction {
		cfg = zap.NewProductionConfig()
	} else {
		cfg = zap.NewDevelopmentConfig()
	}

	if format == "json" {
		cfg.Encoding = "json"
	} else {
		cfg.Encoding = "console"
	}

	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zapcore.InfoLevel
	}
	cfg.Level = zap.NewAtomicLevelAt(zapLevel)

	l, err := cfg.Build(redactingWrapOption)
	if err != nil {
		return nil, err
	}
	Logger = l
	return l, nil
}
