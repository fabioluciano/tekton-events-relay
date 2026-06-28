package bitbucket

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

const (
	testRepoName  = "repo"
	testRunName   = "run-1"
	testCIContext = "tekton/ci"
)

func statusCaptureServer(calls *atomic.Int32, lastPath *atomic.Value, lastBody *atomic.Value) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		lastPath.Store(r.URL.Path)
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		lastBody.Store(payload)
		w.WriteHeader(http.StatusCreated)
	}))
}

func cloudStatusEvent() domain.Event {
	return domain.Event{
		Provider:    providerCloud,
		Repo:        domain.Repo{Workspace: "ws", Name: testRepoName},
		CommitSHA:   "abc123",
		RunName:     testRunName,
		State:       domain.StateFailure,
		Context:     testCIContext,
		Description: "failed",
	}
}

func TestCloudStatusReporter_NameAndType(t *testing.T) {
	r := NewCloudStatusReporter("u", "p", "http://localhost", false, zap.NewNop())
	if r.Name() != "bitbucket-cloud" {
		t.Errorf("Name = %q, want bitbucket-cloud", r.Name())
	}
	if r.Type() != notifier.ActionCommitStatus {
		t.Errorf("Type = %q, want commit_status", r.Type())
	}
}

func TestCloudStatusReporter_SkipsWrongProviderAndMissingFields(t *testing.T) {
	var calls atomic.Int32
	var path, body atomic.Value
	srv := statusCaptureServer(&calls, &path, &body)
	defer srv.Close()

	r := NewCloudStatusReporter("u", "p", srv.URL, false, zap.NewNop())

	e := cloudStatusEvent()
	e.Provider = "github"
	_ = r.Handle(context.Background(), e)

	e = cloudStatusEvent()
	e.CommitSHA = ""
	_ = r.Handle(context.Background(), e)

	e = cloudStatusEvent()
	e.Repo.Workspace, e.Repo.Owner = "", ""
	_ = r.Handle(context.Background(), e)

	if calls.Load() != 0 {
		t.Errorf("API calls = %d, want 0", calls.Load())
	}
}

func TestCloudStatusReporter_PostsMappedState(t *testing.T) {
	var calls atomic.Int32
	var path, body atomic.Value
	srv := statusCaptureServer(&calls, &path, &body)
	defer srv.Close()

	r := NewCloudStatusReporter("u", "p", srv.URL, false, zap.NewNop())
	if err := r.Handle(context.Background(), cloudStatusEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("API calls = %d, want 1", calls.Load())
	}
	if p := path.Load().(string); !strings.Contains(p, "/2.0/repositories/ws/repo/commit/abc123/statuses/build") {
		t.Errorf("path = %q, want cloud build status endpoint", p)
	}
	payload := body.Load().(map[string]any)
	if payload["state"] != "FAILED" {
		t.Errorf("state = %v, want FAILED", payload["state"])
	}
	if payload["key"] != "tekton/ci" {
		t.Errorf("key = %v, want tekton/ci", payload["key"])
	}
}

func TestServerStatusReporter_PostsMappedState(t *testing.T) {
	var calls atomic.Int32
	var path, body atomic.Value
	srv := statusCaptureServer(&calls, &path, &body)
	defer srv.Close()

	r := NewServerStatusReporter("token", srv.URL, false, zap.NewNop())
	e := cloudStatusEvent()
	e.Provider = providerServer
	e.State = domain.StateSuccess
	if err := r.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("API calls = %d, want 1", calls.Load())
	}
	if p := path.Load().(string); !strings.Contains(p, "/rest/build-status/1.0/commits/abc123") {
		t.Errorf("path = %q, want server build status endpoint", p)
	}
	payload := body.Load().(map[string]any)
	if payload["state"] != "SUCCESSFUL" {
		t.Errorf("state = %v, want SUCCESSFUL", payload["state"])
	}
}

func TestServerStatusReporter_SkipsMissingSHA(t *testing.T) {
	var calls atomic.Int32
	var path, body atomic.Value
	srv := statusCaptureServer(&calls, &path, &body)
	defer srv.Close()

	r := NewServerStatusReporter("token", srv.URL, false, zap.NewNop())
	e := cloudStatusEvent()
	e.Provider = providerServer
	e.CommitSHA = ""
	if err := r.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("API calls = %d, want 0", calls.Load())
	}
}

func TestBitbucketStateMaps(t *testing.T) {
	cloud := map[domain.State]string{
		domain.StatePending:  "INPROGRESS",
		domain.StateRunning:  "INPROGRESS",
		domain.StateSuccess:  "SUCCESSFUL",
		domain.StateFailure:  "FAILED",
		domain.StateError:    "FAILED",
		domain.StateCanceled: "STOPPED",
	}
	for in, want := range cloud {
		if got := bitbucketCloudStateMap.Map(in, "INPROGRESS"); got != want {
			t.Errorf("cloud Map(%s) = %s, want %s", in, got, want)
		}
	}
	// Server has no STOPPED state: canceled maps to FAILED.
	if got := bitbucketServerStateMap.Map(domain.StateCanceled, "INPROGRESS"); got != "FAILED" {
		t.Errorf("server Map(canceled) = %s, want FAILED", got)
	}
}
