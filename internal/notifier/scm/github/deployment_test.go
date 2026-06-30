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
	testDeployToken  = "test-token"
	testDeployAPIURL = "https://api.github.com"
)

func TestDeploymentStatusHandler_Name(t *testing.T) {
	h := NewDeploymentStatusHandler(DeploymentStatusConfig{
		Name:   providerGitHub,
		Client: ghTestClient(testDeployToken, testDeployAPIURL),
	}, zap.NewNop())

	if h.Name() != providerGitHub {
		t.Errorf("Name() = %q, want %q", h.Name(), providerGitHub)
	}
}

func TestDeploymentStatusHandler_Type(t *testing.T) {
	h := NewDeploymentStatusHandler(DeploymentStatusConfig{
		Name:   providerGitHub,
		Client: ghTestClient(testDeployToken, testDeployAPIURL),
	}, zap.NewNop())

	if h.Type() != notifier.ActionDeploymentStatus {
		t.Errorf("Type() = %q, want %q", h.Type(), notifier.ActionDeploymentStatus)
	}
}

func TestDeploymentStatusHandler_MapState(t *testing.T) {
	h := NewDeploymentStatusHandler(DeploymentStatusConfig{
		Name:   providerGitHub,
		Client: ghTestClient(testDeployToken, testDeployAPIURL),
	}, zap.NewNop())

	handler := h.(*DeploymentStatusHandler)

	tests := []struct {
		state     domain.State
		wantState string
	}{
		{domain.StatePending, statusQueued},
		{domain.StateRunning, statusInProgress},
		{domain.StateSuccess, stateSuccess},
		{domain.StateFailure, stateFailure},
		{domain.StateError, stateError},
		{domain.StateCanceled, stateInactive},
		{domain.State("unknown"), statusQueued},
	}

	for _, tt := range tests {
		state := handler.mapState(tt.state)
		if state != tt.wantState {
			t.Errorf("mapState(%s) = %q, want %q", tt.state, state, tt.wantState)
		}
	}
}

func TestDeploymentStatusHandler_Handle_Success(t *testing.T) {
	var receivedDeploymentPayload map[string]any
	var receivedStatusPayload map[string]any
	var receivedAuth string

	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")

		if r.Method != "POST" { //nolint:goconst // test string
			t.Errorf("expected POST, got %s", r.Method)
		}

		callCount++

		// First call: create deployment
		if callCount == 1 {
			if r.URL.Path != "/api/v3/repos/myorg/myrepo/deployments" {
				t.Errorf("unexpected deployment path: %s", r.URL.Path)
			}

			if err := json.NewDecoder(r.Body).Decode(&receivedDeploymentPayload); err != nil {
				t.Fatalf("decode deployment body: %v", err)
			}

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 12345}`))
			return
		}

		// Second call: create deployment status
		if callCount == 2 {
			if r.URL.Path != "/api/v3/repos/myorg/myrepo/deployments/12345/statuses" {
				t.Errorf("unexpected status path: %s", r.URL.Path)
			}

			if err := json.NewDecoder(r.Body).Decode(&receivedStatusPayload); err != nil {
				t.Fatalf("decode status body: %v", err)
			}

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 67890}`))
			return
		}

		t.Errorf("unexpected call count: %d", callCount)
	}))
	defer server.Close()

	h := NewDeploymentStatusHandler(DeploymentStatusConfig{
		Name:   providerGitHub,
		Client: ghTestClient("app-token-123", server.URL),
	}, zap.NewNop())

	event := domain.Event{
		Provider:    providerGitHub,
		CommitSHA:   "abc123def",
		Repo:        domain.Repo{Owner: "myorg", Name: "myrepo"},
		RunName:     "deploy-pipeline-run-1",
		RunID:       "uid-12345",
		State:       domain.StateSuccess,
		Context:     "production",
		Description: "Deployment successful",
		TargetURL:   "https://dashboard.example.com/run/1",
	}

	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}

	if receivedAuth != "Bearer app-token-123" {
		t.Errorf("auth = %q, want %q", receivedAuth, "Bearer app-token-123")
	}

	// Verify deployment payload
	if receivedDeploymentPayload["ref"] != "abc123def" {
		t.Errorf("deployment ref = %v, want %q", receivedDeploymentPayload["ref"], "abc123def")
	}
	if receivedDeploymentPayload["environment"] != "production" {
		t.Errorf("deployment environment = %v, want %q", receivedDeploymentPayload["environment"], "production")
	}
	if receivedDeploymentPayload["description"] != "Pipeline: deploy-pipeline-run-1" {
		t.Errorf("deployment description = %v", receivedDeploymentPayload["description"])
	}
	if receivedDeploymentPayload["production_environment"] != true {
		t.Errorf("deployment production_environment = %v, want true", receivedDeploymentPayload["production_environment"])
	}

	// Verify status payload
	if receivedStatusPayload["state"] != stateSuccess {
		t.Errorf("status state = %v, want %q", receivedStatusPayload["state"], stateSuccess)
	}
	if receivedStatusPayload["description"] != "Deployment successful" {
		t.Errorf("status description = %v, want %q", receivedStatusPayload["description"], "Deployment successful")
	}
	if receivedStatusPayload["log_url"] != "https://dashboard.example.com/run/1" {
		t.Errorf("status log_url = %v", receivedStatusPayload["log_url"])
	}
	if receivedStatusPayload["environment_url"] != "https://dashboard.example.com/run/1" {
		t.Errorf("status environment_url = %v", receivedStatusPayload["environment_url"])
	}
}

func TestDeploymentStatusHandler_MissingAnnotations(t *testing.T) {
	h := NewDeploymentStatusHandler(DeploymentStatusConfig{
		Name:   providerGitHub,
		Client: ghTestClient("token", "https://api.github.com"), //nolint:goconst
	}, zap.NewNop())

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
				Context:   "staging",
			},
		},
		{
			name: "missing commit sha",
			event: domain.Event{
				Provider: "providerGitHub", //nolint:goconst
				Repo:     domain.Repo{Owner: "o", Name: "r"},
				Context:  "staging",
			},
		},
		{
			name: "missing repo owner",
			event: domain.Event{
				Provider:  providerGitHub,
				CommitSHA: "abc",
				Repo:      domain.Repo{Name: "r"},
				Context:   "staging",
			},
		},
		{
			name: "missing repo name",
			event: domain.Event{
				Provider:  providerGitHub,
				CommitSHA: "abc",
				Repo:      domain.Repo{Owner: "o"},
				Context:   "staging",
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
