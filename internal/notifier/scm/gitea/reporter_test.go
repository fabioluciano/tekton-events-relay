package gitea

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	if r.Name() != "gitea" {
		t.Errorf("Name() = %q, want gitea", r.Name())
	}
}

func TestNotify(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	r := New(Config{Token: "test-token", BaseURL: server.URL})
	err := r.Notify(context.Background(), domain.Event{
		RunID:      "test",
		State:      domain.StateSuccess,
		CommitSHA:  "abc123",
		Repo:       domain.Repo{Owner: "owner", Name: "repo"},
		APIBaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Notify() error: %v", err)
	}
}
