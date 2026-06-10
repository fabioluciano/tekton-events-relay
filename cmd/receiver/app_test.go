package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewApp(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	validConfig := `
server:
  addr: ":8080"
  metrics_addr: ":9090"
  read_timeout_sec: 30
  write_timeout_sec: 30
  shutdown_timeout_sec: 10
  max_body_size: 1048576
  rate_limit:
    enabled: false
  auth:
    enabled: false
filter:
  ignore_unknown: true
dedupe_size: 10000
max_concurrency: 100
logging:
  level: "info"
tracing:
  enabled: false
scm: {}
notifiers: {}
`
	if err := os.WriteFile(cfgPath, []byte(validConfig), 0600); err != nil {
		t.Fatal(err)
	}

	app, err := newApp(cfgPath)
	if err != nil {
		t.Fatalf("newApp failed: %v", err)
	}
	if app == nil {
		t.Fatal("app is nil")
	}
	if app.cfg == nil {
		t.Fatal("app.cfg is nil")
	}
	if app.log == nil {
		t.Fatal("app.log is nil")
	}
	if app.srv == nil {
		t.Fatal("app.srv is nil")
	}
}

func TestAppShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	validConfig := `
server:
  addr: ":8081"
  metrics_addr: ":9091"
  read_timeout_sec: 30
  write_timeout_sec: 30
  shutdown_timeout_sec: 1
  max_body_size: 1048576
  rate_limit:
    enabled: false
  auth:
    enabled: false
filter:
  ignore_unknown: true
dedupe_size: 10000
max_concurrency: 100
logging:
  level: "info"
tracing:
  enabled: false
scm: {}
notifiers: {}
`
	if err := os.WriteFile(cfgPath, []byte(validConfig), 0600); err != nil {
		t.Fatal(err)
	}

	app, err := newApp(cfgPath)
	if err != nil {
		t.Fatalf("newApp failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go func() {
		_ = app.run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	if err := app.shutdown(); err != nil {
		t.Errorf("shutdown failed: %v", err)
	}
}

func TestBuildDecoders(t *testing.T) {
	reg := buildDecoders()
	if reg == nil {
		t.Fatal("buildDecoders returned nil")
	}
}
