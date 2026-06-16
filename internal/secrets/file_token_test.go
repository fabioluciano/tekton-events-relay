package secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileTokenSource_ReReadsOnEachCall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("first-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	src := NewFileTokenSource(path)

	got, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}
	if got != "first-token" {
		t.Fatalf("Token() = %q, want %q (whitespace must be trimmed)", got, "first-token")
	}

	// Simulate a Kubernetes secret rotation: the mounted file content changes.
	if err := os.WriteFile(path, []byte("rotated-token"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err = src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error after rotation: %v", err)
	}
	if got != "rotated-token" {
		t.Fatalf("Token() = %q, want %q after rotation (must re-read the file)", got, "rotated-token")
	}
}

func TestFileTokenSource_MissingFileErrors(t *testing.T) {
	src := NewFileTokenSource(filepath.Join(t.TempDir(), "does-not-exist"))
	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("expected error for missing token file, got nil")
	}
}

func TestInferPath(t *testing.T) {
	tests := []struct {
		name      string
		explicit  string
		provider  string
		instance  string
		defKey    string
		customKey string
		want      string
		wantErr   bool
	}{
		{name: "explicit path wins", explicit: "/custom/path", want: "/custom/path"},
		{name: "inferred default key", provider: "grafana", instance: "prod", defKey: "token", want: "/etc/secrets/grafana/prod/token"},
		{name: "inferred custom key", provider: "sentry", instance: "prod", defKey: "token", customKey: "api", want: "/etc/secrets/sentry/prod/api"},
		{name: "traversal rejected", provider: "jira", instance: "../evil", defKey: "token", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InferPath(tt.explicit, tt.provider, tt.instance, tt.defKey, tt.customKey)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got path %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("InferPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
