package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	testToken = "secret-token"
	testEmail = "ci@example.com"
	testIssue = "PROJ-123"
)

func testEvent() domain.Event {
	return domain.Event{
		RunName:      "build-run-1",
		PipelineName: "build-and-test",
		Namespace:    "ci",
		State:        domain.StateSuccess,
		CommitSHA:    "0123456789abcdef",
		TargetURL:    "https://tekton.example.com/run/1",
		JiraIssueKey: testIssue,
	}
}

func TestCommentHandler_PostsComment(t *testing.T) {
	var mu sync.Mutex
	var gotPath, gotAuth string
	var gotBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotPath = r.Method + " " + r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Email: testEmail, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, err := NewCommentHandler(client, "", zap.NewNop())
	if err != nil {
		t.Fatalf("NewCommentHandler: %v", err)
	}

	if err := h.Handle(context.Background(), testEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "POST /rest/api/2/issue/PROJ-123/comment" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("expected basic auth (Cloud), got %q", gotAuth)
	}
	for _, want := range []string{"Pipeline success", "build-and-test", "01234567", "https://tekton.example.com/run/1"} {
		if !strings.Contains(gotBody["body"], want) {
			t.Errorf("comment missing %q:\n%s", want, gotBody["body"])
		}
	}
}

func TestCommentHandler_CustomTemplate(t *testing.T) {
	var mu sync.Mutex
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, err := NewCommentHandler(client, "state={{ .State }}", zap.NewNop())
	if err != nil {
		t.Fatalf("NewCommentHandler: %v", err)
	}
	if err := h.Handle(context.Background(), testEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotBody["body"] != "state=success" {
		t.Errorf("custom template not applied: %q", gotBody["body"])
	}
}

func TestCommentHandler_SkipsWithoutIssueKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no request expected without issue key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, _ := NewCommentHandler(client, "", zap.NewNop())

	e := testEvent()
	e.JiraIssueKey = ""
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
}

func TestTransitionHandler_ResolvesByNameAndApplies(t *testing.T) {
	var mu sync.Mutex
	var posted map[string]map[string]string
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotAuth = r.Header.Get("Authorization")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"transitions":[{"id":"11","name":"To Do"},{"id":"31","name":"Done"}]}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&posted)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// no email = Data Center bearer mode
	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, err := NewTransitionHandler(client, "done", zap.NewNop())
	if err != nil {
		t.Fatalf("NewTransitionHandler: %v", err)
	}

	if err := h.Handle(context.Background(), testEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if posted["transition"]["id"] != "31" {
		t.Errorf("expected transition id 31 (Done), got %v", posted)
	}
	if gotAuth != "Bearer "+testToken {
		t.Errorf("expected bearer auth (Data Center), got %q", gotAuth)
	}
}

func TestTransitionHandler_UnavailableTransitionIsSkipped(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"transitions":[{"id":"11","name":"To Do"}]}`))
			return
		}
		posts++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, _ := NewTransitionHandler(client, "Done", zap.NewNop())

	if err := h.Handle(context.Background(), testEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if posts != 0 {
		t.Error("transition POST should be skipped when unavailable")
	}
}

func TestNewTransitionHandler_RequiresTransition(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: "https://x", Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	if _, err := NewTransitionHandler(client, "", zap.NewNop()); err == nil {
		t.Error("expected error for empty transition")
	}
}

func TestNewCommentHandler_InvalidTemplateRejected(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: "https://x", Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	if _, err := NewCommentHandler(client, "{{ .Oops", zap.NewNop()); err == nil {
		t.Error("expected error for invalid template")
	}
}

// rotatingRefresher returns a fresh token on each call, simulating OAuth2
// refresh or a re-read of a rotated mounted secret.
type rotatingRefresher struct {
	mu sync.Mutex
	n  int
}

func (r *rotatingRefresher) Token(_ context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	return "rotated-" + strconv.Itoa(r.n), nil
}

func TestClient_ResolvesTokenPerRequest(t *testing.T) {
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: &rotatingRefresher{}}, zap.NewNop())
	h, err := NewCommentHandler(client, "x", zap.NewNop())
	if err != nil {
		t.Fatalf("NewCommentHandler: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := h.Handle(context.Background(), testEvent()); err != nil {
			t.Fatalf("Handle #%d: %v", i, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(auths) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(auths))
	}
	if auths[0] != "Bearer rotated-1" || auths[1] != "Bearer rotated-2" {
		t.Fatalf("token not refreshed per request: %v", auths)
	}
}
