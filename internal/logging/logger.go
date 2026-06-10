// Package logging provides zap logger configuration and factory.
package logging

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// VerboseOpts contains verbose logging options.
type VerboseOpts struct {
	Caller bool // Show file:line in logs
}

// New creates a *zap.Logger from a log level string and verbose options.
// Valid levels: debug, info, warn, error. Defaults to info if invalid.
func New(levelStr string, verbose VerboseOpts) (*zap.Logger, error) {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		level = zapcore.InfoLevel
	}

	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(level)
	config.Encoding = "json"
	config.EncoderConfig.TimeKey = "time"
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.MessageKey = "msg"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var opts []zap.Option
	if verbose.Caller {
		config.EncoderConfig.CallerKey = "caller"
		opts = append(opts, zap.AddCaller())
	}

	logger, err := config.Build(opts...)
	if err != nil {
		return nil, fmt.Errorf("build zap logger: %w", err)
	}

	return logger, nil
}
