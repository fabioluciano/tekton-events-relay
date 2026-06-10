package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	testStatusToken   = "test-token"
	testStatusName    = "gitlab"
	testCommitSHA     = "abc123"
	testContext       = "tekton/build"
	testDescription   = "Pipeline running"
	testGitLabBaseURL = "https://gitlab.com/api/v4"
)

func newStatusServer(t *testing.T, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(statusCode)
	}))
}

func TestStatusReporter_Name(t *testing.T) {
	r := NewStatusReporter(testStatusToken, testGitLabBaseURL, testStatusName, false, nil)
	if r.Name() != testStatusName {
		t.Errorf("Name() = %q, want %q", r.Name(), testStatusName)
	}
}

func TestStatusReporter_Type(t *testing.T) {
	r := NewStatusReporter(testStatusToken, testGitLabBaseURL, testStatusName, false, nil)
	if r.Type() != notifier.ActionCommitStatus {
		t.Errorf("Type() = %q, want %q", r.Type(), notifier.ActionCommitStatus)
	}
}

func TestStatusReporter_Handle_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("PRIVATE-TOKEN") != testStatusToken {
			t.Errorf("expected PRIVATE-TOKEN header %q, got %q", testStatusToken, r.Header.Get("PRIVATE-TOKEN"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"sha":"abc123","status":"success"}`))
	}))
	defer server.Close()

	r := NewStatusReporter(testStatusToken, server.URL, testStatusName, false, nil)

	event := domain.Event{
		Provider:    testStatusName,
		CommitSHA:   testCommitSHA,
		Repo:        domain.Repo{Owner: testOrgName, Name: testRepoName},
		Context:     testContext,
		Description: "Pipeline succeeded",
		State:       domain.StateSuccess,
	}

	if err := r.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle() unexpected error: %v", err)
	}
}

func TestStatusReporter_Handle_4xx(t *testing.T) {
	server := newStatusServer(t, http.StatusNotFound)
	defer server.Close()

	r := NewStatusReporter(testStatusToken, server.URL, testStatusName, false, nil)

	event := domain.Event{
		Provider:    testStatusName,
		CommitSHA:   testCommitSHA,
		Repo:        domain.Repo{Owner: testOrgName, Name: testRepoName},
		Context:     testContext,
		Description: testDescription,
		State:       domain.StateRunning,
	}

	if err := r.Handle(context.Background(), event); err == nil {
		t.Fatal("Handle() expected error on 404, got nil")
	}
}

func TestStatusReporter_Handle_5xx(t *testing.T) {
	server := newStatusServer(t, http.StatusInternalServerError)
	defer server.Close()

	r := NewStatusReporter(testStatusToken, server.URL, testStatusName, false, nil)

	event := domain.Event{
		Provider:    testStatusName,
		CommitSHA:   testCommitSHA,
		Repo:        domain.Repo{Owner: testOrgName, Name: testRepoName},
		Context:     testContext,
		Description: testDescription,
		State:       domain.StateRunning,
	}

	if err := r.Handle(context.Background(), event); err == nil {
		t.Fatal("Handle() expected error on 500, got nil")
	}
}

func TestStatusReporter_Handle_EmptyCommitSHA(t *testing.T) {
	// No server needed — should return nil before any HTTP call.
	r := NewStatusReporter(testStatusToken, "http://localhost:0", testStatusName, false, nil)

	event := domain.Event{
		Provider:    testStatusName,
		CommitSHA:   "",
		Repo:        domain.Repo{Owner: testOrgName, Name: testRepoName},
		Context:     testContext,
		Description: testDescription,
		State:       domain.StateRunning,
	}

	if err := r.Handle(context.Background(), event); err != nil {
		t.Errorf("Handle() expected nil for empty CommitSHA, got: %v", err)
	}
}

func TestStatusReporter_Handle_WrongProvider(t *testing.T) {
	// No server needed — should return nil without making HTTP call.
	r := NewStatusReporter(testStatusToken, "http://localhost:0", testStatusName, false, nil)

	event := domain.Event{
		Provider:    "github",
		CommitSHA:   testCommitSHA,
		Repo:        domain.Repo{Owner: testOrgName, Name: testRepoName},
		Context:     testContext,
		Description: testDescription,
		State:       domain.StateRunning,
	}

	if err := r.Handle(context.Background(), event); err != nil {
		t.Errorf("Handle() expected nil for wrong provider, got: %v", err)
	}
}

func TestStatusReporter_Handle_ValidationFailure(t *testing.T) {
	// No server needed — validation should fail before HTTP call.
	r := NewStatusReporter(testStatusToken, "http://localhost:0", testStatusName, false, nil)

	event := domain.Event{
		Provider:    testStatusName,
		CommitSHA:   testCommitSHA,
		Repo:        domain.Repo{Owner: testOrgName, Name: testRepoName},
		Context:     "", // empty context triggers validation error
		Description: testDescription,
		State:       domain.StateRunning,
	}

	if err := r.Handle(context.Background(), event); err == nil {
		t.Error("Handle() expected error for empty Context, got nil")
	}
}

// --- NoteHandler success path ---

func TestNoteHandler_Handle_IssueComment_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"body":"Run my-pipeline-run finished"}`))
	}))
	defer server.Close()

	issueNum := 42
	h, err := NewNoteHandler(NoteConfig{
		Token:    testStatusToken,
		BaseURL:  server.URL,
		Name:     testStatusName,
		Template: "Run {{.RunName}} finished",
		NoteType: notifier.ActionIssueComment,
	})
	if err != nil {
		t.Fatalf("NewNoteHandler() unexpected error: %v", err)
	}

	event := domain.Event{
		Provider:    testStatusName,
		CommitSHA:   testCommitSHA,
		Repo:        domain.Repo{Owner: testOrgName, Name: testRepoName},
		RunName:     "my-pipeline-run",
		State:       domain.StateSuccess,
		IssueNumber: &issueNum,
	}

	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatalf("NoteHandler.Handle() unexpected error: %v", err)
	}
}

// --- NoteHandler Name/Type ---

func TestNoteHandler_Name(t *testing.T) {
	h, err := NewNoteHandler(NoteConfig{
		Token:    testStatusToken,
		BaseURL:  testGitLabBaseURL,
		Name:     testStatusName,
		NoteType: notifier.ActionIssueComment,
	})
	if err != nil {
		t.Fatalf("NewNoteHandler() unexpected error: %v", err)
	}
	if h.Name() != testStatusName {
		t.Errorf("NoteHandler.Name() = %q, want %q", h.Name(), testStatusName)
	}
}

func TestNoteHandler_Type(t *testing.T) {
	h, err := NewNoteHandler(NoteConfig{
		Token:    testStatusToken,
		BaseURL:  testGitLabBaseURL,
		Name:     testStatusName,
		NoteType: notifier.ActionPRComment,
	})
	if err != nil {
		t.Fatalf("NewNoteHandler() unexpected error: %v", err)
	}
	if h.Type() != notifier.ActionPRComment {
		t.Errorf("NoteHandler.Type() = %q, want %q", h.Type(), notifier.ActionPRComment)
	}
}

func TestNoteHandler_Handle_PRComment_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"body":"PR run done"}`))
	}))
	defer server.Close()

	prNum := 5
	h, err := NewNoteHandler(NoteConfig{
		Token:    testStatusToken,
		BaseURL:  server.URL,
		Name:     testStatusName,
		Template: "PR run done",
		NoteType: notifier.ActionPRComment,
	})
	if err != nil {
		t.Fatalf("NewNoteHandler() unexpected error: %v", err)
	}

	event := domain.Event{
		Provider:  testStatusName,
		CommitSHA: testCommitSHA,
		Repo:      domain.Repo{Owner: testOrgName, Name: testRepoName},
		RunName:   "my-pipeline-run",
		State:     domain.StateSuccess,
		PRNumber:  &prNum,
	}

	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatalf("NoteHandler.Handle() (PR comment) unexpected error: %v", err)
	}
}

func TestNoteHandler_Handle_WrongProvider(t *testing.T) {
	h, err := NewNoteHandler(NoteConfig{
		Token:    testStatusToken,
		BaseURL:  "http://localhost:0",
		Name:     testStatusName,
		NoteType: notifier.ActionIssueComment,
	})
	if err != nil {
		t.Fatalf("NewNoteHandler() unexpected error: %v", err)
	}

	event := domain.Event{
		Provider: "github",
		Repo:     domain.Repo{Owner: testOrgName, Name: testRepoName},
	}

	if err := h.Handle(context.Background(), event); err != nil {
		t.Errorf("NoteHandler.Handle() expected nil for wrong provider, got: %v", err)
	}
}

// --- LabelHandler Name/Type ---

func TestLabelHandler_Name(t *testing.T) {
	h := NewLabelHandler(LabelConfig{
		Token:   testStatusToken,
		BaseURL: testGitLabBaseURL,
		Name:    testStatusName,
	})
	if h.Name() != testStatusName {
		t.Errorf("LabelHandler.Name() = %q, want %q", h.Name(), testStatusName)
	}
}

func TestLabelHandler_Type(t *testing.T) {
	h := NewLabelHandler(LabelConfig{
		Token:   testStatusToken,
		BaseURL: testGitLabBaseURL,
		Name:    testStatusName,
	})
	if h.Type() != notifier.ActionLabel {
		t.Errorf("LabelHandler.Type() = %q, want %q", h.Type(), notifier.ActionLabel)
	}
}

func TestLabelHandler_Handle_WrongProvider(t *testing.T) {
	h := NewLabelHandler(LabelConfig{
		Token:   testStatusToken,
		BaseURL: "http://localhost:0",
		Name:    testStatusName,
	})

	event := domain.Event{
		Provider: "github",
		Repo:     domain.Repo{Owner: testOrgName, Name: testRepoName},
	}

	if err := h.Handle(context.Background(), event); err != nil {
		t.Errorf("LabelHandler.Handle() expected nil for wrong provider, got: %v", err)
	}
}

//nolint:dupl // intentional duplicate structure testing different label state
func TestLabelHandler_Handle_FailureLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":3,"labels":["ci-failed"]}`))
	}))
	defer server.Close()

	issueNum := 3
	h := NewLabelHandler(LabelConfig{
		Token:        testStatusToken,
		BaseURL:      server.URL,
		Name:         testStatusName,
		SuccessLabel: "ci-passed",
		FailureLabel: "ci-failed",
	})

	event := domain.Event{
		Provider:    testStatusName,
		CommitSHA:   testCommitSHA,
		Repo:        domain.Repo{Owner: testOrgName, Name: testRepoName},
		State:       domain.StateFailure,
		IssueNumber: &issueNum,
	}

	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatalf("LabelHandler.Handle() (failure label) unexpected error: %v", err)
	}
}

// --- LabelHandler success path ---

//nolint:dupl // intentional duplicate structure testing different label state
func TestLabelHandler_Handle_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":7,"labels":["ci-passed"]}`))
	}))
	defer server.Close()

	prNum := 7
	h := NewLabelHandler(LabelConfig{
		Token:        testStatusToken,
		BaseURL:      server.URL,
		Name:         testStatusName,
		SuccessLabel: "ci-passed",
		FailureLabel: "ci-failed",
	})

	event := domain.Event{
		Provider:  testStatusName,
		CommitSHA: testCommitSHA,
		Repo:      domain.Repo{Owner: testOrgName, Name: testRepoName},
		State:     domain.StateSuccess,
		PRNumber:  &prNum,
	}

	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatalf("LabelHandler.Handle() unexpected error: %v", err)
	}
}
