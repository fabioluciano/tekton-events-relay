package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	testHandlerToken = "test-token" //nolint:gosec
	testHandlerOrg   = "test-org"
	testHandlerRepo  = "test-repo"
	testHandlerSHA   = "abc123def456"
)

// mockHTTPDoer implements HTTPDoer for unit tests.
type mockHTTPDoer struct {
	doErr       error
	capturedURL string
	baseURL     string
	token       string
}

func (m *mockHTTPDoer) Do(_ context.Context, _, url string, _ any) error {
	m.capturedURL = url
	return m.doErr
}

func (m *mockHTTPDoer) DoWithResponse(_ context.Context, _, url string, _ any, _ any) error {
	m.capturedURL = url
	return m.doErr
}

func (m *mockHTTPDoer) DoGraphQL(_ context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
	return nil, m.doErr
}

func (m *mockHTTPDoer) BaseURL() string { return m.baseURL }
func (m *mockHTTPDoer) Token() string   { return m.token }

// --- StatusReporter ---

func TestStatusReporter_Name(t *testing.T) {
	r := NewStatusReporter(&mockHTTPDoer{}, zap.NewNop())
	if r.Name() != providerGitHub {
		t.Errorf("Name() = %q, want %q", r.Name(), providerGitHub)
	}
}

func TestStatusReporter_Type(t *testing.T) {
	r := NewStatusReporter(&mockHTTPDoer{}, zap.NewNop())
	if r.Type() != notifier.ActionCommitStatus {
		t.Errorf("Type() = %v, want %v", r.Type(), notifier.ActionCommitStatus)
	}
}

func TestStatusReporter_Handle_WrongProvider(t *testing.T) {
	r := NewStatusReporter(&mockHTTPDoer{}, zap.NewNop())
	err := r.Handle(context.Background(), domain.Event{Provider: "gitlab"}) //nolint:goconst // test string
	if err != nil {
		t.Errorf("expected nil for wrong provider, got: %v", err)
	}
}

func TestStatusReporter_Handle_EmptyCommitSHA(t *testing.T) {
	r := NewStatusReporter(&mockHTTPDoer{}, zap.NewNop())
	err := r.Handle(context.Background(), domain.Event{
		Provider:  providerGitHub,
		CommitSHA: "",
	})
	if err != nil {
		t.Errorf("expected nil for empty SHA, got: %v", err)
	}
}

func TestStatusReporter_Handle_MissingRepo(t *testing.T) {
	r := NewStatusReporter(&mockHTTPDoer{}, zap.NewNop())
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
	mock := &mockHTTPDoer{baseURL: testAPIURL}
	r := NewStatusReporter(mock, zap.NewNop())
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
	mock := &mockHTTPDoer{baseURL: testAPIURL}
	r := NewStatusReporter(mock, zap.NewNop())
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
	h, err := NewIssueCommentHandler(IssueCommentConfig{Token: testHandlerToken}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewIssueCommentHandler() unexpected error: %v", err)
	}
	if h.Name() != providerGitHub {
		t.Errorf("Name() = %q, want %q", h.Name(), providerGitHub)
	}
}

func TestIssueCommentHandler_Type(t *testing.T) {
	h, err := NewIssueCommentHandler(IssueCommentConfig{Token: testHandlerToken}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewIssueCommentHandler() unexpected error: %v", err)
	}
	if h.Type() != notifier.ActionIssueComment {
		t.Errorf("Type() = %v, want %v", h.Type(), notifier.ActionIssueComment)
	}
}

func TestIssueCommentHandler_Handle_WrongProvider(t *testing.T) {
	h, err := NewIssueCommentHandler(IssueCommentConfig{Token: testHandlerToken}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewIssueCommentHandler() unexpected error: %v", err)
	}
	if err := h.Handle(context.Background(), domain.Event{Provider: "gitlab"}); err != nil { //nolint:goconst // test string
		t.Errorf("expected nil for wrong provider, got: %v", err)
	}
}

func TestIssueCommentHandler_Handle_NoIssueNumber(t *testing.T) {
	h, err := NewIssueCommentHandler(IssueCommentConfig{Token: testHandlerToken}, zap.NewNop())
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
		if r.Method != "POST" { //nolint:goconst // test string
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	issueNum := 10
	h, err := NewIssueCommentHandler(IssueCommentConfig{
		Token:    testHandlerToken,
		BaseURL:  server.URL,
		Template: "/tmp/tekton-test-templates/issue.tmpl",
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
		Token:    testHandlerToken,
		BaseURL:  server.URL,
		Template: "/tmp/tekton-test-templates/msg.tmpl",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewIssueCommentHandler() unexpected error: %v", err)
	}

	err = h.Handle(context.Background(), domain.Event{
		Provider:    providerGitHub,
		Repo:        domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		RunName:     "run",
		IssueNumber: &issueNum,
	})
	if err == nil {
		t.Error("expected error on 422, got nil")
	}
}

// --- PRCommentHandler ---

func TestPRCommentHandler_Name(t *testing.T) {
	h, err := NewPRCommentHandler(PRCommentConfig{Token: testHandlerToken}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}
	if h.Name() != providerGitHub {
		t.Errorf("Name() = %q, want %q", h.Name(), providerGitHub)
	}
}

func TestPRCommentHandler_Type(t *testing.T) {
	h, err := NewPRCommentHandler(PRCommentConfig{Token: testHandlerToken}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}
	if h.Type() != notifier.ActionPRComment {
		t.Errorf("Type() = %v, want %v", h.Type(), notifier.ActionPRComment)
	}
}

func TestPRCommentHandler_Handle_WrongProvider(t *testing.T) {
	h, err := NewPRCommentHandler(PRCommentConfig{Token: testHandlerToken}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}
	if err := h.Handle(context.Background(), domain.Event{Provider: "gitlab"}); err != nil { //nolint:goconst // test string
		t.Errorf("expected nil for wrong provider, got: %v", err)
	}
}

func TestPRCommentHandler_Handle_NoPRNumber(t *testing.T) {
	h, err := NewPRCommentHandler(PRCommentConfig{Token: testHandlerToken}, zap.NewNop())
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
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	prNum := 5
	h, err := NewPRCommentHandler(PRCommentConfig{
		Token:    testHandlerToken,
		BaseURL:  server.URL,
		Template: "/tmp/tekton-test-templates/pr.tmpl",
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
		Token:    testHandlerToken,
		BaseURL:  server.URL,
		Template: "/tmp/tekton-test-templates/msg.tmpl",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}

	err = h.Handle(context.Background(), domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		RunName:  "run",
		PRNumber: &prNum,
	})
	if err == nil {
		t.Error("expected error on 500, got nil")
	}
}

// --- LabelHandler ---

func TestLabelHandler_Name(t *testing.T) {
	h := NewLabelHandler(LabelConfig{Token: testHandlerToken}, zap.NewNop())
	if h.Name() != providerGitHub {
		t.Errorf("Name() = %q, want %q", h.Name(), providerGitHub)
	}
}

func TestLabelHandler_Type(t *testing.T) {
	h := NewLabelHandler(LabelConfig{Token: testHandlerToken}, zap.NewNop())
	if h.Type() != notifier.ActionLabel {
		t.Errorf("Type() = %v, want %v", h.Type(), notifier.ActionLabel)
	}
}

func TestLabelHandler_Handle_WrongProvider(t *testing.T) {
	h := NewLabelHandler(LabelConfig{Token: testHandlerToken}, zap.NewNop())
	if err := h.Handle(context.Background(), domain.Event{Provider: "gitlab"}); err != nil { //nolint:goconst // test string
		t.Errorf("expected nil for wrong provider, got: %v", err)
	}
}

func TestLabelHandler_Handle_NoNumber(t *testing.T) {
	h := NewLabelHandler(LabelConfig{Token: testHandlerToken}, zap.NewNop())
	err := h.Handle(context.Background(), domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		State:    domain.StateSuccess,
	})
	if err != nil {
		t.Errorf("expected nil when no issue/PR number, got: %v", err)
	}
}

func TestLabelHandler_Handle_SuccessLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	prNum := 7
	h := NewLabelHandler(LabelConfig{
		Token:        testHandlerToken,
		BaseURL:      server.URL,
		SuccessLabel: "ci-passed",
		FailureLabel: "ci-failed",
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

func TestLabelHandler_Handle_FailureLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	issueNum := 3
	h := NewLabelHandler(LabelConfig{
		Token:        testHandlerToken,
		BaseURL:      server.URL,
		SuccessLabel: "ci-passed",
		FailureLabel: "ci-failed",
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

func TestLabelHandler_Handle_RunningState_Skip(t *testing.T) {
	h := NewLabelHandler(LabelConfig{
		Token:        testHandlerToken,
		SuccessLabel: "ci-passed",
		FailureLabel: "ci-failed",
	}, zap.NewNop())

	prNum := 1
	err := h.Handle(context.Background(), domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		State:    domain.StateRunning,
		PRNumber: &prNum,
	})
	if err != nil {
		t.Errorf("expected nil for running state, got: %v", err)
	}
}

// --- Client.GetBaseURL / GetToken ---

func TestClient_GetBaseURL(t *testing.T) {
	c := NewClient(testHandlerToken, testAPIURL, false, nil, false)
	if c.BaseURL() != testAPIURL {
		t.Errorf("GetBaseURL() = %q, want %q", c.BaseURL(), testAPIURL)
	}
}

func TestClient_GetToken(t *testing.T) {
	c := NewClient(testHandlerToken, testAPIURL, false, nil, false)
	if c.Token() != testHandlerToken {
		t.Errorf("GetToken() = %q, want %q", c.Token(), testHandlerToken)
	}
}
