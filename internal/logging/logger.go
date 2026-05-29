// Package logging provides zap logger configuration and factory.
package logging

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New creates a *zap.Logger from a log level string.
// Valid levels: debug, info, warn, error. Defaults to info if invalid.
func New(levelStr string, debug bool) (*zap.Logger, error) {
	var level zapcore.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel
	}

	// Debug mode forces debug level
	if debug {
		level = zapcore.DebugLevel
	}

	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(level)
	config.Encoding = "json"
	config.EncoderConfig.TimeKey = "time"
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.MessageKey = "msg"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// Add caller info in debug mode
	if debug {
		config.EncoderConfig.CallerKey = "caller"
		logger, err := config.Build(zap.AddCaller())
		if err != nil {
			return nil, fmt.Errorf("build zap logger: %w", err)
		}
		return logger, nil
	}

	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("build zap logger: %w", err)
	}

	return logger, nil
}
