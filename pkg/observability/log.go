package observability

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is a global logger instance for convenience, though passing logger by context is preferred.
var Logger *zap.Logger

func init() {
	Logger, _ = zap.NewProduction()
}

// NewLogger creates a new configured zap logger with level and format (json/text)
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

	l, err := cfg.Build()
	if err != nil {
		return nil, err
	}
	Logger = l
	return l, nil
}
