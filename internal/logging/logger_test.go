package logging

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestNew_ValidLevels(t *testing.T) {
	tests := []struct {
		name     string
		levelStr string
		want     zapcore.Level
	}{
		{"debug level", "debug", zapcore.DebugLevel},
		{"info level", "info", zapcore.InfoLevel},
		{"warn level", "warn", zapcore.WarnLevel},
		{"error level", "error", zapcore.ErrorLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.levelStr, VerboseOpts{})
			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			if logger == nil {
				t.Fatal("New() returned nil logger")
			}

			// Verify logger level matches expected
			if !logger.Core().Enabled(tt.want) {
				t.Errorf("logger level not enabled for %v", tt.want)
			}
		})
	}
}

func TestNew_InvalidLevel(t *testing.T) {
	logger, err := New("invalid", VerboseOpts{})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if logger == nil {
		t.Fatal("New() returned nil logger")
	}

	// Should default to info level
	if !logger.Core().Enabled(zapcore.InfoLevel) {
		t.Error("invalid level should default to info")
	}
}

func TestNew_EmptyLevel(t *testing.T) {
	logger, err := New("", VerboseOpts{})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if logger == nil {
		t.Fatal("New() returned nil logger")
	}

	// Should default to info level
	if !logger.Core().Enabled(zapcore.InfoLevel) {
		t.Error("empty level should default to info")
	}
}

func TestNew_WithCaller(t *testing.T) {
	logger, err := New("info", VerboseOpts{Caller: true})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if logger == nil {
		t.Fatal("New() returned nil logger")
	}

	// Verify caller option is enabled by checking if logger has caller skip
	// We can't directly check the option, but we can verify the logger works
	logger.Info("test message with caller")
}

func TestNew_WithoutCaller(t *testing.T) {
	logger, err := New("info", VerboseOpts{Caller: false})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if logger == nil {
		t.Fatal("New() returned nil logger")
	}

	logger.Info("test message without caller")
}

func TestNew_AllLevelsWithCaller(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}
	for _, level := range levels {
		t.Run(level+"_with_caller", func(t *testing.T) {
			logger, err := New(level, VerboseOpts{Caller: true})
			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			if logger == nil {
				t.Fatal("New() returned nil logger")
			}
		})
	}
}

func TestVerboseOpts(t *testing.T) {
	tests := []struct {
		name    string
		opts    VerboseOpts
		wantErr bool
	}{
		{"caller enabled", VerboseOpts{Caller: true}, false},
		{"caller disabled", VerboseOpts{Caller: false}, false},
		{"zero value", VerboseOpts{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New("info", tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && logger == nil {
				t.Error("New() returned nil logger when no error expected")
			}
		})
	}
}

func TestNew_LoggerCanWrite(t *testing.T) {
	logger, err := New("debug", VerboseOpts{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Verify logger can write at different levels
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")
}

func TestNew_LoggerWithFields(t *testing.T) {
	logger, err := New("info", VerboseOpts{Caller: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Verify logger can write with fields
	logger.Info("message with fields",
		zap.String("key1", "value1"),
		zap.Int("key2", 42),
	)
}
