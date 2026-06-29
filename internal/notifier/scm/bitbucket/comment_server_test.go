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

func newServerCommentHandler(t *testing.T, baseURL string) notifier.ActionHandler {
	t.Helper()
	h, err := NewServerCommentHandler(ServerCommentConfig{
		Token:    "token",
		BaseURL:  baseURL,
		Template: "Run {{.RunName}}: {{.State}}",
		Log:      zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewServerCommentHandler: %v", err)
	}
	return h
}

func TestServerCommentHandler_NameAndType(t *testing.T) {
	h := newServerCommentHandler(t, "http://localhost")
	if h.Name() != "bitbucket-server" {
		t.Errorf("Name = %q, want bitbucket-server", h.Name())
	}
	if h.Type() != notifier.ActionPRComment {
		t.Errorf("Type = %q, want pr_comment", h.Type())
	}
}

func TestServerCommentHandler_PostsComment(t *testing.T) {
	var calls atomic.Int32
	var lastPath atomic.Value
	var lastBody atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		lastPath.Store(r.URL.Path)
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		lastBody.Store(payload)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	h := newServerCommentHandler(t, srv.URL)
	pr := 9
	e := domain.Event{
		Provider: providerServer,
		Repo:     domain.Repo{Project: "PROJ", Name: testRepoName},
		RunName:  testRunName,
		PRNumber: &pr,
		State:    domain.StateSuccess,
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("API calls = %d, want 1", calls.Load())
	}
	if p := lastPath.Load().(string); !strings.Contains(p, "/rest/api/1.0/projects/PROJ/repos/repo/pull-requests/9/comments") {
		t.Errorf("path = %q, want server PR comments endpoint", p)
	}
	if body := lastBody.Load().(map[string]string); !strings.Contains(body["text"], "run-1") {
		t.Errorf("comment text = %q, want rendered template", body["text"])
	}
}

func TestServerCommentHandler_SkipsMissingFields(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	h := newServerCommentHandler(t, srv.URL)
	pr := 9

	e := domain.Event{Provider: "github", Repo: domain.Repo{Project: "P", Name: "r"}, PRNumber: &pr}
	_ = h.Handle(context.Background(), e)

	e = domain.Event{Provider: providerServer, Repo: domain.Repo{Project: "P", Name: "r"}} // no PR
	_ = h.Handle(context.Background(), e)

	e = domain.Event{Provider: providerServer, Repo: domain.Repo{Name: "r"}, PRNumber: &pr} // no project
	_ = h.Handle(context.Background(), e)

	if calls.Load() != 0 {
		t.Errorf("API calls = %d, want 0", calls.Load())
	}
}
