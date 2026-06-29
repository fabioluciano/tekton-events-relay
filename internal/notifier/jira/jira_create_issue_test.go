package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

func failureEvent() domain.Event {
	return domain.Event{
		RunName:      "build-run-1",
		RunID:        "uid-123",
		PipelineName: "build-and-test",
		Namespace:    "ci",
		State:        domain.StateFailure,
		CommitSHA:    "0123456789abcdef",
		TargetURL:    "https://tekton.example.com/run/1",
		Description:  "Step build failed with exit code 1",
	}
}

func TestCreateIssue(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"total":0}`))
			return
		}
		gotPath = r.Method + " " + r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"PROJ-456"}`))
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, err := NewCreateIssueHandler(client, "PROJ", "Bug", zap.NewNop())
	if err != nil {
		t.Fatalf("NewCreateIssueHandler: %v", err)
	}

	if err := h.Handle(context.Background(), failureEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "POST /rest/api/3/issue" {
		t.Errorf("path = %q", gotPath)
	}

	fields, ok := gotBody["fields"].(map[string]any)
	if !ok {
		t.Fatalf("missing fields in body: %+v", gotBody)
	}
	if fields["summary"] != "Pipeline failure: build-run-1" {
		t.Errorf("summary = %v", fields["summary"])
	}
	labels, ok := fields["labels"].([]any)
	if !ok {
		t.Fatalf("missing labels: %+v", fields)
	}
	if len(labels) != 1 || labels[0] != "tekton-relay:uid-123" {
		t.Errorf("labels = %v", labels)
	}
}

func TestCreateIssueDeduplicated(t *testing.T) {
	creates := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"total":1}`))
			return
		}
		creates++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"PROJ-456"}`))
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, err := NewCreateIssueHandler(client, "PROJ", "Bug", zap.NewNop())
	if err != nil {
		t.Fatalf("NewCreateIssueHandler: %v", err)
	}

	if err := h.Handle(context.Background(), failureEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if creates != 0 {
		t.Errorf("expected 0 creates (dedup), got %d", creates)
	}
}

func TestCreateIssue_SkipsPendingRunning(t *testing.T) {
	creates := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		creates++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"PROJ-1"}`))
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, err := NewCreateIssueHandler(client, "PROJ", "Bug", zap.NewNop())
	if err != nil {
		t.Fatalf("NewCreateIssueHandler: %v", err)
	}

	for _, state := range []domain.State{domain.StatePending, domain.StateRunning} {
		creates = 0
		e := failureEvent()
		e.State = state
		if err := h.Handle(context.Background(), e); err != nil {
			t.Fatalf("Handle state=%s: %v", state, err)
		}
		if creates != 0 {
			t.Errorf("state %s: expected 0 creates, got %d", state, creates)
		}
	}
}

func TestCreateIssue_UsesDefaultIssueType(t *testing.T) {
	var mu sync.Mutex
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"total":0}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"X-1"}`))
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, err := NewCreateIssueHandler(client, "X", "", zap.NewNop())
	if err != nil {
		t.Fatalf("NewCreateIssueHandler: %v", err)
	}

	if err := h.Handle(context.Background(), failureEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	fields := gotBody["fields"].(map[string]any)
	issuetype := fields["issuetype"].(map[string]any)
	if issuetype["name"] != "Bug" {
		t.Errorf("default issue type = %v, want Bug", issuetype["name"])
	}
}

func TestNewCreateIssueHandler_RequiresProjectKey(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: testBaseURL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	if _, err := NewCreateIssueHandler(client, "", "Bug", zap.NewNop()); err == nil {
		t.Error("expected error for empty project_key")
	}
}

func TestCreateIssue_Name(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: testBaseURL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, _ := NewCreateIssueHandler(client, "P", "Bug", zap.NewNop())
	if h.Name() != "jira" {
		t.Errorf("Name() = %q, want jira", h.Name())
	}
	if h.Type() != "jira_create_issue" {
		t.Errorf("Type() = %q, want jira_create_issue", h.Type())
	}
}

func TestLinkCommit(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotPath = r.Method + " " + r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, err := NewLinkCommitHandler(client, "PROJ-123", zap.NewNop())
	if err != nil {
		t.Fatalf("NewLinkCommitHandler: %v", err)
	}

	e := failureEvent()
	e.APIBaseURL = "https://github.com/org/repo"
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "POST /rest/api/3/issue/PROJ-123/remotelink" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["globalId"] != "tekton-relay:uid-123:0123456789abcdef" {
		t.Errorf("globalId = %v", gotBody["globalId"])
	}
	obj, ok := gotBody["object"].(map[string]any)
	if !ok {
		t.Fatalf("missing object: %+v", gotBody)
	}
	if obj["url"] != "https://tekton.example.com/run/1" {
		t.Errorf("url = %v", obj["url"])
	}
}

func TestLinkCommitSkipsWhenNoSHA(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posts++
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, err := NewLinkCommitHandler(client, "PROJ-123", zap.NewNop())
	if err != nil {
		t.Fatalf("NewLinkCommitHandler: %v", err)
	}

	e := failureEvent()
	e.CommitSHA = ""
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if posts != 0 {
		t.Errorf("expected 0 posts (no SHA), got %d", posts)
	}
}

func TestLinkCommit_UsesAPIBaseURLWhenNoTargetURL(t *testing.T) {
	var mu sync.Mutex
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, err := NewLinkCommitHandler(client, "PROJ-123", zap.NewNop())
	if err != nil {
		t.Fatalf("NewLinkCommitHandler: %v", err)
	}

	e := failureEvent()
	e.TargetURL = ""
	e.APIBaseURL = "https://github.com/org/repo"
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	obj := gotBody["object"].(map[string]any)
	want := fmt.Sprintf("https://github.com/org/repo/commit/%s", e.CommitSHA)
	if obj["url"] != want {
		t.Errorf("url = %v, want %v", obj["url"], want)
	}
}

func TestNewLinkCommitHandler_RequiresIssueKey(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: testBaseURL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	if _, err := NewLinkCommitHandler(client, "", zap.NewNop()); err == nil {
		t.Error("expected error for empty issue_key")
	}
}

func TestLinkCommit_Name(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: testBaseURL, Token: scm.NewStaticToken(testToken)}, zap.NewNop())
	h, _ := NewLinkCommitHandler(client, "X-1", zap.NewNop())
	if h.Name() != "jira" {
		t.Errorf("Name() = %q, want jira", h.Name())
	}
	if h.Type() != "jira_link_commit" {
		t.Errorf("Type() = %q, want jira_link_commit", h.Type())
	}
}
