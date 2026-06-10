package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestRetryConfig_DefaultsApplied(t *testing.T) {
	var cfg Config
	applyDefaults(&cfg)

	if cfg.Retry.MaxAttempts != 4 {
		t.Errorf("MaxAttempts = %d, want 4", cfg.Retry.MaxAttempts)
	}
	if cfg.Retry.InitialBackoff != 250*time.Millisecond {
		t.Errorf("InitialBackoff = %v, want 250ms", cfg.Retry.InitialBackoff)
	}
	if cfg.Retry.MaxBackoff != 30*time.Second {
		t.Errorf("MaxBackoff = %v, want 30s", cfg.Retry.MaxBackoff)
	}
}

func TestRetryConfig_ParsesFromYAML(t *testing.T) {
	raw := `
retry:
  max_attempts: 6
  initial_backoff: 500ms
  max_backoff: 1m
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	applyDefaults(&cfg)

	if cfg.Retry.MaxAttempts != 6 {
		t.Errorf("MaxAttempts = %d, want 6", cfg.Retry.MaxAttempts)
	}
	if cfg.Retry.InitialBackoff != 500*time.Millisecond {
		t.Errorf("InitialBackoff = %v, want 500ms", cfg.Retry.InitialBackoff)
	}
	if cfg.Retry.MaxBackoff != time.Minute {
		t.Errorf("MaxBackoff = %v, want 1m", cfg.Retry.MaxBackoff)
	}
}
