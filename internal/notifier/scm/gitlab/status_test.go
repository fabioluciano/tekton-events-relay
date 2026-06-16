package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
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

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := NewClient(testStatusToken, baseURL, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestStatusReporter_Name(t *testing.T) {
	client := newTestClient(t, testGitLabBaseURL)
	r, err := NewStatusReporter(client, testStatusName, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Name() != testStatusName {
		t.Errorf("Name() = %q, want %q", r.Name(), testStatusName)
	}
}

func TestStatusReporter_Type(t *testing.T) {
	client := newTestClient(t, testGitLabBaseURL)
	r, err := NewStatusReporter(client, testStatusName, nil)
	if err != nil {
		t.Fatal(err)
	}
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

	client := newTestClient(t, server.URL)
	r, err := NewStatusReporter(client, testStatusName, nil)
	if err != nil {
		t.Fatal(err)
	}

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

	client := newTestClient(t, server.URL)
	r, err := NewStatusReporter(client, testStatusName, nil)
	if err != nil {
		t.Fatal(err)
	}

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

	client := newTestClient(t, server.URL)
	r, err := NewStatusReporter(client, testStatusName, nil)
	if err != nil {
		t.Fatal(err)
	}

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
	client := newTestClient(t, "http://localhost:0")
	r, err := NewStatusReporter(client, testStatusName, nil)
	if err != nil {
		t.Fatal(err)
	}

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
	client := newTestClient(t, "http://localhost:0")
	r, err := NewStatusReporter(client, testStatusName, nil)
	if err != nil {
		t.Fatal(err)
	}

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
	client := newTestClient(t, "http://localhost:0")
	r, err := NewStatusReporter(client, testStatusName, nil)
	if err != nil {
		t.Fatal(err)
	}

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

// --- LabelHandler Name/Type ---

func TestLabelHandler_Name(t *testing.T) {
	client := newTestClient(t, testGitLabBaseURL)
	h, err := NewLabelHandler(LabelConfig{
		Client: client,
		Name:   testStatusName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.Name() != testStatusName {
		t.Errorf("LabelHandler.Name() = %q, want %q", h.Name(), testStatusName)
	}
}

func TestLabelHandler_Type(t *testing.T) {
	client := newTestClient(t, testGitLabBaseURL)
	h, err := NewLabelHandler(LabelConfig{
		Client: client,
		Name:   testStatusName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.Type() != notifier.ActionLabel {
		t.Errorf("LabelHandler.Type() = %q, want %q", h.Type(), notifier.ActionLabel)
	}
}

func TestLabelHandler_Handle_WrongProvider(t *testing.T) {
	client := newTestClient(t, "http://localhost:0")
	h, err := NewLabelHandler(LabelConfig{
		Client: client,
		Name:   testStatusName,
	})
	if err != nil {
		t.Fatal(err)
	}

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
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			// ListLabels call - return empty list for any GET
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if r.Method == "POST" {
			// CreateLabel call
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1,"name":"ci-passed","color":"#0e8a16"}`))
			return
		}
		if r.Method == "PUT" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":3,"labels":["ci-failed"]}`))
			return
		}
		t.Errorf("unexpected method: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	issueNum := 3
	h, err := NewLabelHandler(LabelConfig{
		Client: client,
		Name:   testStatusName,
		Labels: scm.LabelSet{Add: []scm.Label{{Name: "ci-passed"}}},
	})
	if err != nil {
		t.Fatal(err)
	}

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
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			// ListLabels call - return empty list for any GET
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if r.Method == "POST" {
			// CreateLabel call
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1,"name":"ci-passed","color":"#0e8a16"}`))
			return
		}
		if r.Method == "PUT" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":7,"labels":["ci-passed"]}`))
			return
		}
		t.Errorf("unexpected method: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	prNum := 7
	h, err := NewLabelHandler(LabelConfig{
		Client: client,
		Name:   testStatusName,
		Labels: scm.LabelSet{Add: []scm.Label{{Name: "ci-passed"}}},
	})
	if err != nil {
		t.Fatal(err)
	}

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
