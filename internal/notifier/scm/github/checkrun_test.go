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
	testToken  = "test-token"
	testAPIURL = "https://api.github.com"
)

func TestCheckRunHandler_Name(t *testing.T) {
	h, err := NewCheckRunHandler(CheckRunConfig{
		InstName: providerGitHub,
		Client:   ghTestClient(testToken, testAPIURL),
		Name:     "my-check",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewCheckRunHandler: %v", err)
	}

	if h.Name() != providerGitHub {
		t.Errorf("Name() = %q, want %q", h.Name(), providerGitHub)
	}
}

func TestCheckRunHandler_Type(t *testing.T) {
	h, err := NewCheckRunHandler(CheckRunConfig{
		InstName: providerGitHub,
		Client:   ghTestClient(testToken, testAPIURL),
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewCheckRunHandler: %v", err)
	}

	if h.Type() != notifier.ActionCheckRun {
		t.Errorf("Type() = %q, want %q", h.Type(), notifier.ActionCheckRun)
	}
}

func TestCheckRunHandler_MapState(t *testing.T) {
	h, err := NewCheckRunHandler(CheckRunConfig{
		InstName: providerGitHub,
		Client:   ghTestClient(testToken, testAPIURL),
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewCheckRunHandler: %v", err)
	}

	handler := h.(*CheckRunHandler)

	tests := []struct {
		state      domain.State
		wantStatus string
		wantConc   string
	}{
		{domain.StatePending, statusQueued, ""},
		{domain.StateRunning, statusInProgress, ""},
		{domain.StateSuccess, statusCompleted, stateSuccess},
		{domain.StateFailure, statusCompleted, stateFailure},
		{domain.StateError, statusCompleted, stateFailure},
		{domain.StateCanceled, statusCompleted, stateCancelled},
		{domain.State("unknown"), statusQueued, ""},
	}

	for _, tt := range tests {
		status, conclusion := handler.mapState(tt.state)
		if status != tt.wantStatus {
			t.Errorf("mapState(%s) status = %q, want %q", tt.state, status, tt.wantStatus)
		}
		if conclusion != tt.wantConc {
			t.Errorf("mapState(%s) conclusion = %q, want %q", tt.state, conclusion, tt.wantConc)
		}
	}
}

func TestCheckRunHandler_Handle_Success(t *testing.T) {
	var receivedPayload map[string]any
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")

		if r.Method != "POST" { //nolint:goconst // test string
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v3/repos/myorg/myrepo/check-runs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 1}`))
	}))
	defer server.Close()

	h, err := NewCheckRunHandler(CheckRunConfig{
		InstName: providerGitHub,
		Client:   ghTestClient("app-token-123", server.URL),
		Name:     "tekton/build",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewCheckRunHandler: %v", err)
	}

	event := domain.Event{
		Provider:  providerGitHub,
		CommitSHA: "abc123def",
		Repo:      domain.Repo{Owner: "myorg", Name: "myrepo"},
		RunName:   "build-pipeline-run-1",
		RunID:     "uid-12345",
		State:     domain.StateSuccess,
	}

	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if receivedAuth != "Bearer app-token-123" {
		t.Errorf("auth = %q, want %q", receivedAuth, "Bearer app-token-123")
	}

	if receivedPayload["name"] != "tekton/build" {
		t.Errorf("name = %v, want %q", receivedPayload["name"], "tekton/build")
	}
	if receivedPayload["head_sha"] != "abc123def" {
		t.Errorf("head_sha = %v, want %q", receivedPayload["head_sha"], "abc123def")
	}
	if receivedPayload["status"] != statusCompleted {
		t.Errorf("status = %v, want %q", receivedPayload["status"], statusCompleted)
	}
	if receivedPayload["conclusion"] != stateSuccess {
		t.Errorf("conclusion = %v, want %q", receivedPayload["conclusion"], stateSuccess)
	}
	if receivedPayload["external_id"] != "uid-12345" {
		t.Errorf("external_id = %v, want %q", receivedPayload["external_id"], "uid-12345")
	}

	// Verify output/summary exists
	output, ok := receivedPayload["output"].(map[string]any)
	if !ok {
		t.Fatal("expected output field in payload")
	}
	if output["title"] != "Pipeline: build-pipeline-run-1" {
		t.Errorf("output.title = %v", output["title"])
	}
}

func TestCheckRunHandler_Template(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)

		output, ok := payload["output"].(map[string]any)
		if !ok {
			t.Fatal("expected output field")
		}

		summary, _ := output["summary"].(string)
		if summary != "Run: my-run | State: running" {
			t.Errorf("summary = %q, want %q", summary, "Run: my-run | State: running")
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 1}`))
	}))
	defer server.Close()

	h, err := NewCheckRunHandler(CheckRunConfig{
		InstName: providerGitHub,
		Client:   ghTestClient("token", server.URL),
		Template: "/tmp/tekton-test-templates-github/checkrun.tmpl",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewCheckRunHandler: %v", err)
	}

	event := domain.Event{
		Provider:  providerGitHub,
		CommitSHA: "sha1",
		Repo:      domain.Repo{Owner: "o", Name: "r"},
		RunName:   "my-run",
		State:     domain.StateRunning,
	}

	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle: %v", err)
	}
}

func TestCheckRunHandler_MissingFields(t *testing.T) {
	h, err := NewCheckRunHandler(CheckRunConfig{
		InstName: providerGitHub,
		Client:   ghTestClient("token", "https://api.github.com"), //nolint:goconst
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewCheckRunHandler: %v", err)
	}

	tests := []struct {
		name  string
		event domain.Event
	}{
		{
			name: "wrong provider",
			event: domain.Event{
				Provider:  "gitlab", //nolint:goconst // test string
				CommitSHA: "abc",    //nolint:goconst
				Repo:      domain.Repo{Owner: "o", Name: "r"},
			},
		},
		{
			name: "missing commit sha",
			event: domain.Event{
				Provider: providerGitHub,
				Repo:     domain.Repo{Owner: "o", Name: "r"},
			},
		},
		{
			name: "missing repo owner",
			event: domain.Event{
				Provider:  providerGitHub,
				CommitSHA: "abc",
				Repo:      domain.Repo{Name: "r"},
			},
		},
		{
			name: "missing repo name",
			event: domain.Event{
				Provider:  providerGitHub,
				CommitSHA: "abc",
				Repo:      domain.Repo{Owner: "o"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := h.Handle(context.Background(), tt.event)
			if err != nil {
				t.Errorf("expected nil (skip), got error: %v", err)
			}
		})
	}
}

func TestCheckRunHandler_InvalidTemplate(t *testing.T) {
	_, err := NewCheckRunHandler(CheckRunConfig{
		Client:   ghTestClient("token", "https://api.github.com"),
		Template: "{{.Invalid",
	}, zap.NewNop())
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}
