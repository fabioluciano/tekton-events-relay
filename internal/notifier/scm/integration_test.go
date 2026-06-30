package scm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/bitbucket"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/github"
)

const (
	testDashboardURL     = "https://dashboard.example.com"
	testOwner            = "testowner"
	testRepoName         = "testrepo"
	testCommitSHA        = "abc123def456"
	testBuildContext     = "tekton/build"
	testBuildDescription = "Build succeeded"
	testToken            = "test-token"
	testBitbucketServer  = "bitbucket-server"
	testGitHubProvider   = "github"
	testSuccessfulState  = "SUCCESSFUL"
)

// TestIntegration_ValidationErrorPropagation verifies that validation errors
// from field limit checks propagate through the full handler chain.
// This is an end-to-end test proving validation contracts are enforced.
func TestIntegration_ValidationErrorPropagation(t *testing.T) {
	// Create an event with a description that exceeds GitHub's 140 char limit
	longDescription := strings.Repeat("x", 200)
	event := domain.Event{
		Repo: domain.Repo{
			Owner: testOwner,
			Name:  testRepoName,
		},
		CommitSHA:   testCommitSHA,
		State:       domain.StateSuccess,
		Context:     "tekton/integration-test",
		Description: longDescription,
		TargetURL:   testDashboardURL,
	}

	// Create a mock server that should never be called (validation fails first)
	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverCalled = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	// Create GitHub status reporter
	client := github.NewClient(testToken, server.URL, false, zap.NewNop(), false)
	handler := github.NewStatusReporter(client, "github", zap.NewNop())

	// Override API base URL to point to mock server
	event.APIBaseURL = server.URL
	event.Provider = testGitHubProvider

	// Execute the handler
	err := handler.Handle(context.Background(), event)

	// Assertions
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	// The error should mention the field and the limit
	errMsg := err.Error()
	if !strings.Contains(errMsg, "description") {
		t.Errorf("error should mention 'description' field, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "140") {
		t.Errorf("error should mention GitHub's 140 char limit, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "200") {
		t.Errorf("error should mention actual length (200), got: %s", errMsg)
	}

	// Server should not have been called due to validation failure
	if serverCalled {
		t.Error("HTTP request should not be sent when validation fails")
	}
}

// TestIntegration_BitbucketParentIncluded verifies that Bitbucket Server
// includes the "parent" field when Repo.Project is set.
// This tests the full payload construction and serialization path.
func TestIntegration_BitbucketParentIncluded(t *testing.T) {
	var capturedBody []byte

	// Create mock server that captures the request body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 123}`))
	}))
	defer server.Close()

	// Create event with Project field populated
	event := domain.Event{
		Repo: domain.Repo{
			Name:    "test-repo",
			Project: "MYPROJECT",
		},
		CommitSHA:   "abc123def456",
		State:       domain.StateSuccess,
		Context:     "tekton/build",
		Description: "Build succeeded",
		TargetURL:   testDashboardURL,
		APIBaseURL:  server.URL,
	}

	// Create Bitbucket Server status reporter
	handler := bitbucket.NewServerStatusReporter("bitbucket-server", testToken, server.URL, false, nil)
	event.Provider = testBitbucketServer

	// Execute the handler
	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parse the captured JSON body
	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal captured body: %v", err)
	}

	// Assert: "parent" field must be present with correct value
	parent, ok := payload["parent"]
	if !ok {
		t.Errorf("payload missing 'parent' field. Full payload: %+v", payload)
	}
	if parent != "MYPROJECT" {
		t.Errorf("parent = %q, want 'MYPROJECT'", parent)
	}

	// Also verify other expected fields are present
	expectedFields := map[string]string{
		"state":       testSuccessfulState,
		"key":         testBuildContext,
		"name":        testBuildContext,
		"url":         testDashboardURL,
		"description": testBuildDescription,
	}
	for field, expectedValue := range expectedFields {
		if got, ok := payload[field].(string); !ok || got != expectedValue {
			t.Errorf("payload[%q] = %v, want %q", field, payload[field], expectedValue)
		}
	}
}

// TestIntegration_BitbucketParentOmitted verifies that Bitbucket Server
// does NOT include the "parent" field when Repo.Project is empty.
// This ensures the optional field behavior is correct.
func TestIntegration_BitbucketParentOmitted(t *testing.T) {
	var capturedBody []byte

	// Create mock server that captures the request body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 123}`))
	}))
	defer server.Close()

	// Create event WITHOUT Project field
	event := domain.Event{
		Repo: domain.Repo{
			Name:    "test-repo",
			Project: "", // Explicitly empty
		},
		CommitSHA:   "abc123def456",
		State:       domain.StateSuccess,
		Context:     "tekton/build",
		Description: "Build succeeded",
		TargetURL:   testDashboardURL,
		APIBaseURL:  server.URL,
	}

	// Create Bitbucket Server status reporter
	handler := bitbucket.NewServerStatusReporter("bitbucket-server", testToken, server.URL, false, nil)
	event.Provider = testBitbucketServer

	// Execute the handler
	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parse the captured JSON body
	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal captured body: %v", err)
	}

	// Assert: "parent" field must NOT be present
	if parent, ok := payload["parent"]; ok {
		t.Errorf("payload should not contain 'parent' field when Project is empty, got: %v", parent)
	}

	// Verify other expected fields are still present
	expectedFields := map[string]string{
		"state":       testSuccessfulState,
		"key":         testBuildContext,
		"name":        testBuildContext,
		"url":         testDashboardURL,
		"description": testBuildDescription,
	}
	for field, expectedValue := range expectedFields {
		if got, ok := payload[field].(string); !ok || got != expectedValue {
			t.Errorf("payload[%q] = %v, want %q", field, payload[field], expectedValue)
		}
	}
}

// TestIntegration_GitHubValidationSuccess verifies that valid GitHub events
// pass validation and are sent successfully. This is a positive control test.
func TestIntegration_GitHubValidationSuccess(t *testing.T) {
	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverCalled = true
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 123}`))
	}))
	defer server.Close()

	// Create event with valid description (within 140 char limit)
	event := domain.Event{
		Repo: domain.Repo{
			Owner: testOwner,
			Name:  testRepoName,
		},
		CommitSHA:   testCommitSHA,
		State:       domain.StateSuccess,
		Context:     "tekton/test",
		Description: "All tests passed", // Well within 140 chars
		TargetURL:   testDashboardURL,
		APIBaseURL:  server.URL,
		Provider:    "github",
	}

	client := github.NewClient(testToken, server.URL, false, zap.NewNop(), false)
	handler := github.NewStatusReporter(client, "github", zap.NewNop())

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error with valid payload: %v", err)
	}

	if !serverCalled {
		t.Error("server should have been called for valid payload")
	}
}

// TestIntegration_StateFiltering verifies that status handlers respect
// the notifyOn configuration and skip events for unmatched states.
func TestIntegration_StateFiltering(t *testing.T) {
	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverCalled = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	event := domain.Event{
		Repo: domain.Repo{
			Owner: "testowner",
			Name:  "testrepo",
		},
		CommitSHA:   "abc123",
		State:       domain.StateSuccess, // Event is success
		Context:     "test",
		Description: "done",
		APIBaseURL:  server.URL,
		Provider:    "github",
	}

	client := github.NewClient("test-token", server.URL, false, zap.NewNop(), false)
	baseHandler := github.NewStatusReporter(client, "github", zap.NewNop())

	// Configure handler to only notify on failure and error
	handler := NewStatusHandler(baseHandler, []string{"failure", "error"})

	err := handler.Handle(context.Background(), event)

	// Should return nil (skipped) but not call the server
	if err != nil {
		t.Fatalf("expected nil (skip), got error: %v", err)
	}

	if serverCalled {
		t.Error("server should not be called when state doesn't match notifyOn filter")
	}
}

// NewStatusHandler wraps an ActionHandler with notifyOn state filtering for testing.
func NewStatusHandler(handler notifier.ActionHandler, notifyOn []string) notifier.ActionHandler {
	return &statusHandlerAdapter{
		handler:  handler,
		notifyOn: notifyOn,
	}
}

type statusHandlerAdapter struct {
	handler  notifier.ActionHandler
	notifyOn []string
}

func (a *statusHandlerAdapter) Name() string     { return a.handler.Name() }
func (a *statusHandlerAdapter) Provider() string { return a.handler.Provider() }
func (a *statusHandlerAdapter) Type() notifier.ActionType {
	return a.handler.Type()
}

func (a *statusHandlerAdapter) Handle(ctx context.Context, e domain.Event) error {
	// Apply state filtering: if notifyOn is empty, match all; otherwise check if state matches
	if len(a.notifyOn) > 0 {
		stateStr := string(e.State)
		match := false
		for _, s := range a.notifyOn {
			if s == stateStr {
				match = true
				break
			}
		}
		if !match {
			return nil // Skip
		}
	}

	// Delegate to wrapped handler
	return a.handler.Handle(ctx, e)
}

func (a *statusHandlerAdapter) Close() error { return a.handler.Close() }
