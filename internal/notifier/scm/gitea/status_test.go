package gitea

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

func giteaAPIServer(t *testing.T, calls *atomic.Int32, lastStatus *atomic.Value) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "1.22.0"})
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/statuses/"):
			calls.Add(1)
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			lastStatus.Store(payload)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func giteaStatusEvent() domain.Event {
	return domain.Event{
		Provider:    providerGitea,
		Repo:        domain.Repo{Owner: "org", Name: "repo"},
		CommitSHA:   "abc123",
		RunName:     "run-1",
		State:       domain.StateRunning,
		Context:     "tekton/ci",
		Description: "running",
	}
}

func TestStatusReporter_NameAndType(t *testing.T) {
	r := NewStatusReporter("token", "http://localhost", false, zap.NewNop())
	if r.Name() != "gitea" {
		t.Errorf("Name = %q, want gitea", r.Name())
	}
	if r.Type() != notifier.ActionCommitStatus {
		t.Errorf("Type = %q, want commit_status", r.Type())
	}
}

func TestStatusReporter_SkipsWrongProvider(t *testing.T) {
	var calls atomic.Int32
	var last atomic.Value
	srv := giteaAPIServer(t, &calls, &last)
	defer srv.Close()

	r := NewStatusReporter("token", srv.URL, false, zap.NewNop())
	e := giteaStatusEvent()
	e.Provider = "github"
	if err := r.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("API calls = %d, want 0 (provider mismatch)", calls.Load())
	}
}

func TestStatusReporter_SkipsMissingFields(t *testing.T) {
	var calls atomic.Int32
	var last atomic.Value
	srv := giteaAPIServer(t, &calls, &last)
	defer srv.Close()

	r := NewStatusReporter("token", srv.URL, false, zap.NewNop())
	for _, mutate := range []func(*domain.Event){
		func(e *domain.Event) { e.CommitSHA = "" },
		func(e *domain.Event) { e.Repo.Owner = "" },
		func(e *domain.Event) { e.Repo.Name = "" },
	} {
		e := giteaStatusEvent()
		mutate(&e)
		if err := r.Handle(context.Background(), e); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}
	if calls.Load() != 0 {
		t.Errorf("API calls = %d, want 0 (missing fields skip)", calls.Load())
	}
}

func TestStatusReporter_PostsMappedState(t *testing.T) {
	var calls atomic.Int32
	var last atomic.Value
	srv := giteaAPIServer(t, &calls, &last)
	defer srv.Close()

	r := NewStatusReporter("token", srv.URL, false, zap.NewNop())
	if err := r.Handle(context.Background(), giteaStatusEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("API calls = %d, want 1", calls.Load())
	}
	payload := last.Load().(map[string]any)
	if payload["state"] != "pending" { //nolint:goconst // test assertion
		t.Errorf("state = %v, want pending (running maps to pending)", payload["state"])
	}
	if payload["context"] != "tekton/ci" {
		t.Errorf("context = %v, want tekton/ci", payload["context"])
	}
}

func TestGiteaStateMap(t *testing.T) {
	cases := map[domain.State]string{
		domain.StatePending:  "pending",
		domain.StateRunning:  "pending",
		domain.StateSuccess:  "success",
		domain.StateFailure:  "failure",
		domain.StateError:    "error",
		domain.StateCanceled: "error",
	}
	for in, want := range cases {
		if got := giteaStateMap.Map(in, "pending"); got != want {
			t.Errorf("Map(%s) = %s, want %s", in, got, want)
		}
	}
}
