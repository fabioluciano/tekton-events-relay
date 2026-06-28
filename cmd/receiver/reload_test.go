package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

const reloadBaseConfig = `
server:
  addr: "127.0.0.1:0"
  auth:
    enabled: false
filter:
  ignore_unknown: true
logging:
  level: "info"
scm: {}
notifiers: {}
`

func writeReloadConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReload_SwapsHandlers(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	writeReloadConfig(t, cfgPath, reloadBaseConfig)

	// Secret file for the notifier added on reload.
	urlFile := filepath.Join(tmpDir, "webhook-url")
	if err := os.WriteFile(urlFile, []byte("https://hooks.example.com/x"), 0o600); err != nil {
		t.Fatal(err)
	}

	a, err := newApp(cfgPath)
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	defer a.cleanup()

	if got := len(a.regHolder.Names()); got != 0 {
		t.Fatalf("initial handlers = %d, want 0", got)
	}

	writeReloadConfig(t, cfgPath, `
server:
  addr: "127.0.0.1:0"
  auth:
    enabled: false
filter:
  ignore_unknown: true
logging:
  level: "info"
scm: {}
notifiers:
  webhook:
    - name: generic
      enabled: true
      url_file: `+urlFile+`
`)

	a.reload()

	names := a.regHolder.Names()
	if len(names) != 1 {
		t.Fatalf("handlers after reload = %v, want 1 webhook handler", names)
	}
	if got := testutil.ToFloat64(a.collectors.ConfigReloads.WithLabelValues("success")); got != 1 {
		t.Errorf("config_reloads{success} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(a.collectors.ConfigReloadLastTimestamp); got == 0 {
		t.Errorf("config_reload_last_timestamp = 0, want non-zero")
	}
}

func TestReload_InvalidConfigKeepsCurrent(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	writeReloadConfig(t, cfgPath, reloadBaseConfig)

	a, err := newApp(cfgPath)
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	defer a.cleanup()

	writeReloadConfig(t, cfgPath, "server: [broken")
	a.reload()

	if got := len(a.regHolder.Names()); got != 0 {
		t.Errorf("handlers after failed reload = %d, want 0 (unchanged)", got)
	}
	if got := testutil.ToFloat64(a.collectors.ConfigReloads.WithLabelValues("failure")); got != 1 {
		t.Errorf("config_reloads{failure} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(a.collectors.ConfigReloads.WithLabelValues("success")); got != 0 {
		t.Errorf("config_reloads{success} = %v, want 0", got)
	}
	// Also check new histogram and gauge were recorded even on failure.
	if got := testutil.ToFloat64(a.collectors.ConfigReloadLastTimestamp); got == 0 {
		t.Errorf("config_reload_last_timestamp = 0, want non-zero even on failure")
	}
}
