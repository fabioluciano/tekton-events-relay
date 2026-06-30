package sourcehut

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

func sourcehutEvent() domain.Event {
	return domain.Event{
		Provider:    providerName,
		Repo:        domain.Repo{Owner: "user", Name: "repo"},
		CommitSHA:   "abc123",
		RunName:     "run-1",
		State:       domain.StateSuccess,
		Context:     "ci",
		Description: "done",
	}
}

func TestStatusReporter_NameAndType(t *testing.T) {
	r := NewStatusReporter("sourcehut", "token", "https://builds.sr.ht", false, zap.NewNop())
	if r.Name() != "sourcehut" {
		t.Errorf("Name = %q, want sourcehut", r.Name())
	}
	if r.Type() != notifier.ActionCommitStatus {
		t.Errorf("Type = %q, want commit_status", r.Type())
	}
}

func TestStatusReporter_SkipsWrongProviderAndMissingFields(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	r := NewStatusReporter("sourcehut", "token", srv.URL, false, zap.NewNop())

	e := sourcehutEvent()
	e.Provider = "github"
	_ = r.Handle(context.Background(), e)

	e = sourcehutEvent()
	e.CommitSHA = ""
	_ = r.Handle(context.Background(), e)

	e = sourcehutEvent()
	e.Repo.Owner = ""
	_ = r.Handle(context.Background(), e)

	if calls.Load() != 0 {
		t.Errorf("API calls = %d, want 0", calls.Load())
	}
}

func TestStatusReporter_SubmitsJobManifest(t *testing.T) {
	var calls atomic.Int32
	var lastBody atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		lastBody.Store(payload)
		if !strings.HasSuffix(r.URL.Path, "/api/jobs") {
			t.Errorf("path = %q, want /api/jobs", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	r := NewStatusReporter("sourcehut", "token", srv.URL, false, zap.NewNop())
	if err := r.Handle(context.Background(), sourcehutEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("API calls = %d, want 1", calls.Load())
	}

	payload := lastBody.Load().(map[string]any)
	manifest, _ := payload["manifest"].(string)
	if !strings.Contains(manifest, "~user/repo#abc123") {
		t.Errorf("manifest missing source ref: %q", manifest)
	}
	if !strings.Contains(manifest, "success") {
		t.Errorf("manifest missing mapped state: %q", manifest)
	}
	if !strings.Contains(manifest, "exit 0") {
		t.Errorf("manifest exit code for success should be 0: %q", manifest)
	}
}

func TestStatusReporter_FailureProducesNonZeroExit(t *testing.T) {
	var lastBody atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		lastBody.Store(payload)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	r := NewStatusReporter("sourcehut", "token", srv.URL, false, zap.NewNop())
	e := sourcehutEvent()
	e.State = domain.StateFailure
	if err := r.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	manifest, _ := lastBody.Load().(map[string]any)["manifest"].(string)
	if strings.Contains(manifest, "exit 0") {
		t.Errorf("failure manifest must not exit 0: %q", manifest)
	}
}
