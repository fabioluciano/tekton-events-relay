package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func TestDeploymentHandler_CreatesDeployment(t *testing.T) {
	var calls int
	var lastBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/deployments") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls++
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
	}))
	defer srv.Close()

	client, err := NewClient("t", srv.URL, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewDeploymentHandler(DeploymentConfig{
		Client: client, Name: "gitlab-main", Log: zap.NewNop(),
	})
	if err != nil {
		t.Fatal(err)
	}

	e := domain.Event{
		Provider:  "gitlab-main",
		Repo:      domain.Repo{ID: "42"},
		CommitSHA: "abc123",
		Context:   "staging",
		State:     domain.StateSuccess,
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if lastBody["environment"] != "staging" || lastBody["status"] != "success" {
		t.Errorf("body = %v, want staging/success", lastBody)
	}

	// Pending state is not a deployment status: skip.
	e.State = domain.StatePending
	_ = h.Handle(context.Background(), e)
	// Wrong provider: skip.
	e.State = domain.StateSuccess
	e.Provider = "github"
	_ = h.Handle(context.Background(), e)
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (skips)", calls)
	}
}
