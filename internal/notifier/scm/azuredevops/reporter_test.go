package azuredevops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func TestNew(t *testing.T) {
	cfg := Config{Token: "test-token"}
	r := New(cfg)
	if r == nil {
		t.Fatal("expected reporter")
	}
}

func TestName(t *testing.T) {
	r := New(Config{Token: "test"})
	if r.Name() != "azure-devops" {
		t.Errorf("Name() = %q, want azure-devops", r.Name())
	}
}

func TestNotify(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := New(Config{Token: "test-token", BaseURL: server.URL})
	err := r.Notify(context.Background(), domain.Event{
		RunID:      "test",
		State:      domain.StateSuccess,
		CommitSHA:  "abc123",
		Repo:       domain.Repo{Org: "org", Project: "project", Name: "repo"},
		APIBaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Notify() error: %v", err)
	}
}

func TestReporter_payload_ValidationErrors(t *testing.T) {
	r := New(Config{Token: "test-token"})

	tests := []struct {
		name        string
		event       domain.Event
		expectError string
	}{
		{
			name: "description exceeds 4000 chars",
			event: domain.Event{
				State:       domain.StateSuccess,
				Context:     "test",
				Description: strings.Repeat("a", 4001),
			},
			expectError: `field "description" exceeds limit (4000 chars, got 4001)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.payload(tt.event)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.expectError {
				t.Errorf("error = %q, expected %q", err.Error(), tt.expectError)
			}
		})
	}
}
