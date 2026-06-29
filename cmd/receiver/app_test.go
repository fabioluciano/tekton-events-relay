package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
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

	waitForServerReady(t, "http://localhost:8081/healthz")

	app.shutdown()
}

func TestBuildDecoders(t *testing.T) {
	reg := buildDecoders()
	if reg == nil {
		t.Fatal("buildDecoders returned nil")
	}
}

func waitForServerReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatalf("server at %s did not start within deadline", url)
		case <-ticker.C:
			resp, err := http.Get(url) //nolint:gosec // test-only local URL
			if err == nil {
				_ = resp.Body.Close()
				return
			}
		}
	}
}

func TestShutdownClosesHandlers(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	writeReloadConfig(t, cfgPath, `
server:
  addr: "127.0.0.1:0"
  shutdown_timeout_sec: 1
  auth:
    enabled: false
filter:
  ignore_unknown: true
logging:
  level: "info"
scm: {}
notifiers: {}
`)

	a, err := newApp(cfgPath)
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}

	spy := &closeSpy{name: "shutdown-spy"}
	wrapped := notifier.NewConditionalHandler(spy, nil, zap.NewNop())

	reg := notifier.NewRegistry()
	reg.Register(wrapped)
	a.regHolder.p.Store(reg)

	a.shutdown()

	if spy.closeCount != 1 {
		t.Errorf("expected closeCount=1, got %d", spy.closeCount)
	}

	spy.closeCount = 0
	a.shutdown()

	if spy.closeCount != 0 {
		t.Errorf("double-close detected: expected closeCount=0, got %d", spy.closeCount)
	}
}
