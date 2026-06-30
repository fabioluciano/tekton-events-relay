package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gh "github.com/google/go-github/v68/github"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	testHandlerToken = "test-token" //nolint:gosec
	testHandlerOrg   = "test-org"
	testHandlerRepo  = "test-repo"
	testHandlerSHA   = "abc123def456"
	testRunName      = "run"
	testHTTPPost     = "POST"
)

// ghTestClient builds a real *Client as an HTTPDoer for handler tests.
func ghTestClient(token, baseURL string) HTTPDoer {
	return NewClient(token, baseURL, false, zap.NewNop(), false)
}

// mockHTTPDoer implements HTTPDoer for unit tests.
type mockHTTPDoer struct {
	doErr error
}

func (m *mockHTTPDoer) DoGraphQL(_ context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
	return nil, m.doErr
}

// GH returns nil: guard-path unit tests return before any SDK call.
func (m *mockHTTPDoer) GH() *gh.Client { return nil }

// --- StatusReporter ---

func TestStatusReporter_Name(t *testing.T) {
	r := NewStatusReporter(&mockHTTPDoer{}, "github", zap.NewNop())
	if r.Name() != providerGitHub {
		t.Errorf("Name() = %q, want %q", r.Name(), providerGitHub)
	}
}

func TestStatusReporter_Type(t *testing.T) {
	r := NewStatusReporter(&mockHTTPDoer{}, "github", zap.NewNop())
	if r.Type() != notifier.ActionCommitStatus {
		t.Errorf("Type() = %v, want %v", r.Type(), notifier.ActionCommitStatus)
	}
}

func TestStatusReporter_Handle_WrongProvider(t *testing.T) {
	r := NewStatusReporter(&mockHTTPDoer{}, "github", zap.NewNop())
	err := r.Handle(context.Background(), domain.Event{Provider: "gitlab"}) //nolint:goconst // test string
	if err != nil {
		t.Errorf("expected nil for wrong provider, got: %v", err)
	}
}

func TestStatusReporter_Handle_EmptyCommitSHA(t *testing.T) {
	r := NewStatusReporter(&mockHTTPDoer{}, "github", zap.NewNop())
	err := r.Handle(context.Background(), domain.Event{
		Provider:  providerGitHub,
		CommitSHA: "",
	})
	if err != nil {
		t.Errorf("expected nil for empty SHA, got: %v", err)
	}
}

func TestStatusReporter_Handle_MissingRepo(t *testing.T) {
	r := NewStatusReporter(&mockHTTPDoer{}, "github", zap.NewNop())
	err := r.Handle(context.Background(), domain.Event{
		Provider:  providerGitHub,
		CommitSHA: testHandlerSHA,
		Repo:      domain.Repo{},
	})
	if err != nil {
		t.Errorf("expected nil for missing repo, got: %v", err)
	}
}

func TestStatusReporter_Handle_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != testHTTPPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// go-github WithEnterpriseURLs routes status calls to /api/v3/repos/{owner}/{repo}/statuses/{sha}
		wantPath := "/api/v3/repos/" + testHandlerOrg + "/" + testHandlerRepo + "/statuses/" + testHandlerSHA
		if r.URL.Path != wantPath {
			t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	r := NewStatusReporter(ghTestClient(testHandlerToken, server.URL), "github", zap.NewNop())
	err := r.Handle(context.Background(), domain.Event{
		Provider:    providerGitHub,
		CommitSHA:   testHandlerSHA,
		Repo:        domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		State:       domain.StateSuccess,
		Description: "Pipeline succeeded",
		Context:     "tekton/build",
	})
	if err != nil {
		t.Errorf("Handle() unexpected error: %v", err)
	}
}

func TestStatusReporter_Handle_ValidationFailure(t *testing.T) {
	mock := &mockHTTPDoer{}
	r := NewStatusReporter(mock, "github", zap.NewNop())
	// GitHub status_description limit is 140 chars — exceed it to trigger validation error
	longDesc := "x" + string(make([]byte, 140))
	err := r.Handle(context.Background(), domain.Event{
		Provider:    providerGitHub,
		CommitSHA:   testHandlerSHA,
		Repo:        domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		State:       domain.StateRunning,
		Description: longDesc,
		Context:     "tekton/build",
	})
	if err == nil {
		t.Error("expected validation error for over-limit description, got nil")
	}
}

// --- IssueCommentHandler ---

func TestIssueCommentHandler_Name(t *testing.T) {
	h, err := NewIssueCommentHandler(IssueCommentConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewIssueCommentHandler() unexpected error: %v", err)
	}
	if h.Name() != providerGitHub {
		t.Errorf("Name() = %q, want %q", h.Name(), providerGitHub)
	}
}

func TestIssueCommentHandler_Type(t *testing.T) {
	h, err := NewIssueCommentHandler(IssueCommentConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewIssueCommentHandler() unexpected error: %v", err)
	}
	if h.Type() != notifier.ActionIssueComment {
		t.Errorf("Type() = %v, want %v", h.Type(), notifier.ActionIssueComment)
	}
}

func TestIssueCommentHandler_Handle_WrongProvider(t *testing.T) {
	h, err := NewIssueCommentHandler(IssueCommentConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewIssueCommentHandler() unexpected error: %v", err)
	}
	if err := h.Handle(context.Background(), domain.Event{Provider: "gitlab"}); err != nil { //nolint:goconst // test string
		t.Errorf("expected nil for wrong provider, got: %v", err)
	}
}

func TestIssueCommentHandler_Handle_NoIssueNumber(t *testing.T) {
	h, err := NewIssueCommentHandler(IssueCommentConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewIssueCommentHandler() unexpected error: %v", err)
	}
	if err := h.Handle(context.Background(), domain.Event{Provider: providerGitHub}); err != nil {
		t.Errorf("expected nil for no issue number, got: %v", err)
	}
}

//nolint:dupl // intentional duplicate structure testing different handler
func TestIssueCommentHandler_Handle_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != testHTTPPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	issueNum := 10
	h, err := NewIssueCommentHandler(IssueCommentConfig{
		Client:   ghTestClient(testHandlerToken, server.URL),
		Template: "/tmp/tekton-test-templates-github/issue.tmpl",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewIssueCommentHandler() unexpected error: %v", err)
	}

	err = h.Handle(context.Background(), domain.Event{
		Provider:    providerGitHub,
		Repo:        domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		RunName:     "my-run",
		IssueNumber: &issueNum,
		State:       domain.StateSuccess,
	})
	if err != nil {
		t.Fatalf("Handle() unexpected error: %v", err)
	}
}

func TestIssueCommentHandler_Handle_4xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer server.Close()

	issueNum := 1
	h, err := NewIssueCommentHandler(IssueCommentConfig{
		Client:   ghTestClient(testHandlerToken, server.URL),
		Template: "/tmp/tekton-test-templates-github/msg.tmpl",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewIssueCommentHandler() unexpected error: %v", err)
	}

	err = h.Handle(context.Background(), domain.Event{
		Provider:    providerGitHub,
		Repo:        domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		RunName:     testRunName,
		IssueNumber: &issueNum,
	})
	if err == nil {
		t.Error("expected error on 422, got nil")
	}
}

// --- PRCommentHandler ---

func TestPRCommentHandler_Name(t *testing.T) {
	h, err := NewPRCommentHandler(PRCommentConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}
	if h.Name() != providerGitHub {
		t.Errorf("Name() = %q, want %q", h.Name(), providerGitHub)
	}
}

func TestPRCommentHandler_Type(t *testing.T) {
	h, err := NewPRCommentHandler(PRCommentConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}
	if h.Type() != notifier.ActionPRComment {
		t.Errorf("Type() = %v, want %v", h.Type(), notifier.ActionPRComment)
	}
}

func TestPRCommentHandler_Handle_WrongProvider(t *testing.T) {
	h, err := NewPRCommentHandler(PRCommentConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}
	if err := h.Handle(context.Background(), domain.Event{Provider: "gitlab"}); err != nil { //nolint:goconst // test string
		t.Errorf("expected nil for wrong provider, got: %v", err)
	}
}

func TestPRCommentHandler_Handle_NoPRNumber(t *testing.T) {
	h, err := NewPRCommentHandler(PRCommentConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}
	if err := h.Handle(context.Background(), domain.Event{Provider: providerGitHub}); err != nil {
		t.Errorf("expected nil for no PR number, got: %v", err)
	}
}

//nolint:dupl // intentional duplicate structure testing different handler
func TestPRCommentHandler_Handle_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != testHTTPPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	prNum := 5
	h, err := NewPRCommentHandler(PRCommentConfig{
		Client:   ghTestClient(testHandlerToken, server.URL),
		Template: "/tmp/tekton-test-templates-github/pr.tmpl",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}

	err = h.Handle(context.Background(), domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		RunName:  "my-run",
		PRNumber: &prNum,
		State:    domain.StateSuccess,
	})
	if err != nil {
		t.Fatalf("Handle() unexpected error: %v", err)
	}
}

func TestPRCommentHandler_Handle_5xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	prNum := 3
	h, err := NewPRCommentHandler(PRCommentConfig{
		Client:   ghTestClient(testHandlerToken, server.URL),
		Template: "/tmp/tekton-test-templates-github/msg.tmpl",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}

	err = h.Handle(context.Background(), domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		RunName:  testRunName,
		PRNumber: &prNum,
	})
	if err == nil {
		t.Error("expected error on 500, got nil")
	}
}

// --- LabelHandler ---

func TestLabelHandler_Name(t *testing.T) {
	h := NewLabelHandler(LabelConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	if h.Name() != providerGitHub {
		t.Errorf("Name() = %q, want %q", h.Name(), providerGitHub)
	}
}

func TestLabelHandler_Type(t *testing.T) {
	h := NewLabelHandler(LabelConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	if h.Type() != notifier.ActionLabel {
		t.Errorf("Type() = %v, want %v", h.Type(), notifier.ActionLabel)
	}
}

func TestLabelHandler_Handle_WrongProvider(t *testing.T) {
	h := NewLabelHandler(LabelConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	if err := h.Handle(context.Background(), domain.Event{Provider: "gitlab"}); err != nil { //nolint:goconst // test string
		t.Errorf("expected nil for wrong provider, got: %v", err)
	}
}

func TestLabelHandler_Handle_NoNumber(t *testing.T) {
	h := NewLabelHandler(LabelConfig{Name: providerGitHub, Client: ghTestClient(testHandlerToken, "")}, zap.NewNop())
	err := h.Handle(context.Background(), domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		State:    domain.StateSuccess,
	})
	if err != nil {
		t.Errorf("expected nil when no issue/PR number, got: %v", err)
	}
}

func TestLabelHandler_Handle_AddLabelOnPR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != testHTTPPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	prNum := 7
	h := NewLabelHandler(LabelConfig{
		Client: ghTestClient(testHandlerToken, server.URL),
		Labels: scm.LabelSet{Add: []scm.Label{{Name: "ci-passed"}}},
	}, zap.NewNop())

	err := h.Handle(context.Background(), domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		State:    domain.StateSuccess,
		PRNumber: &prNum,
	})
	if err != nil {
		t.Fatalf("Handle() unexpected error: %v", err)
	}
}

func TestLabelHandler_Handle_AddLabelOnIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	issueNum := 3
	h := NewLabelHandler(LabelConfig{
		Client: ghTestClient(testHandlerToken, server.URL),
		Labels: scm.LabelSet{Add: []scm.Label{{Name: "ci-passed"}}},
	}, zap.NewNop())

	err := h.Handle(context.Background(), domain.Event{
		Provider:    providerGitHub,
		Repo:        domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		State:       domain.StateFailure,
		IssueNumber: &issueNum,
	})
	if err != nil {
		t.Fatalf("Handle() unexpected error: %v", err)
	}
}

func TestLabelHandler_Handle_UpdateLabelColor(t *testing.T) {
	var getCount, patchCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET /repos/org/repo/labels/passed - return existing label with wrong color
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/labels/passed") {
			getCount++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"passed","color":"ffffff"}`))
			return
		}
		// PATCH /repos/org/repo/labels/passed - update color
		if r.Method == "PATCH" && strings.Contains(r.URL.Path, "/labels/passed") {
			patchCount++
			w.WriteHeader(http.StatusOK)
			return
		}
		// POST /repos/org/repo/issues/7/labels - apply to issue
		if r.Method == testHTTPPost && strings.Contains(r.URL.Path, "/issues/7/labels") {
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	prNum := 7
	h := NewLabelHandler(LabelConfig{
		Client: ghTestClient(testHandlerToken, server.URL),
		Labels: scm.LabelSet{Add: []scm.Label{{Name: "passed", Color: "0e8a16"}}},
	}, zap.NewNop())

	err := h.Handle(context.Background(), domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		State:    domain.StateSuccess,
		PRNumber: &prNum,
	})
	if err != nil {
		t.Fatalf("Handle() unexpected error: %v", err)
	}

	if getCount != 1 {
		t.Errorf("expected 1 GET call, got %d", getCount)
	}
	if patchCount != 1 {
		t.Errorf("expected 1 PATCH call to update color, got %d", patchCount)
	}
}

func TestLabelHandler_Handle_EmptyLabels_Skip(t *testing.T) {
	h := NewLabelHandler(LabelConfig{
		Client: ghTestClient(testHandlerToken, ""),
	}, zap.NewNop())

	prNum := 1
	err := h.Handle(context.Background(), domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		State:    domain.StateRunning,
		PRNumber: &prNum,
	})
	if err != nil {
		t.Errorf("expected nil for empty label set, got: %v", err)
	}
}
