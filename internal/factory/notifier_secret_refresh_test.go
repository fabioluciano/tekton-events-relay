package factory

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestResolveFileRefresher_readsNotifierSecretFileEachTime(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		instance    string
		initial     string
		rotated     string
		explicitKey string
	}{
		{name: "datadog api key", provider: "datadog", instance: "metrics", initial: "dd-v1", rotated: "dd-v2", explicitKey: "api_key"},
		{name: "pagerduty integration key", provider: "pagerduty", instance: "oncall", initial: "pd-v1", rotated: "pd-v2", explicitKey: "integration_key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: a factory-resolved file refresher over a mounted-secret style file.
			secretFile := filepath.Join(t.TempDir(), tt.explicitKey)
			writeSecretFile(t, secretFile, tt.initial)

			refresher, err := resolveFileRefresher(secretFile, tt.explicitKey, tt.provider, tt.instance, zap.NewNop())
			if err != nil {
				t.Fatalf("resolveFileRefresher() error = %v", err)
			}

			// When: the secret file changes after factory construction.
			first, err := refresher.Token(context.Background())
			if err != nil {
				t.Fatalf("first Token() error = %v", err)
			}
			writeSecretFile(t, secretFile, tt.rotated)
			second, err := refresher.Token(context.Background())
			if err != nil {
				t.Fatalf("second Token() error = %v", err)
			}

			// Then: the same refresher observes the rotated file value.
			if first != tt.initial || second != tt.rotated {
				t.Fatalf("Token() values = [%q %q], want [%q %q]", first, second, tt.initial, tt.rotated)
			}
		})
	}
}

func writeSecretFile(t *testing.T, path string, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
}
