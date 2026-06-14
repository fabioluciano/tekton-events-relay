package pagerduty

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	testKeyValue         = "test-key"
	testSeverityCrit     = "critical"
	testSeverityWarning  = "warning"
	testSeverityError    = "error"
	testNamePagerDuty    = "pagerduty"
	testActionTrigger    = "trigger"
	testActionResolve    = "resolve"
	testRunID123         = "run-123"
	testRunID456         = "run-456"
	testRunID789         = "run-789"
	testStatusSuccess    = "success"
	testNamespaceDefault = "default"
	testContextBuild     = "build"
	testDescBuildFailed  = "Build failed"
	testNamespaceProd    = "production"
	testContextTest      = "test"
	testTargetURL        = "https://tekton.example.com/run-123"
)

func TestNew(t *testing.T) {
	cfg := Config{IntegrationKey: testKeyValue}
	n := New(cfg, nil)
	if n == nil {
		t.Fatal("expected notifier")
	}
}

func TestNewWithDefaultSeverity(t *testing.T) {
	cfg := Config{IntegrationKey: testKeyValue}
	n := New(cfg, nil)
	if n.cfg.Severity != testSeverityCrit {
		t.Errorf("expected default severity 'critical', got %q", n.cfg.Severity)
	}
}

func TestNewWithCustomSeverity(t *testing.T) {
	cfg := Config{IntegrationKey: testKeyValue, Severity: testSeverityWarning}
	n := New(cfg, nil)
	if n.cfg.Severity != testSeverityWarning {
		t.Errorf("expected severity 'warning', got %q", n.cfg.Severity)
	}
}

func TestName(t *testing.T) {
	n := New(Config{IntegrationKey: "test"}, nil)
	if n.Name() != testNamePagerDuty {
		t.Errorf("Name() = %q, want pagerduty", n.Name())
	}
}

func TestNotifyWithFailureState(t *testing.T) {
	receivedPayload := make(map[string]any)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer server.Close()

	n := New(Config{IntegrationKey: testKeyValue}, nil)
	n.base.BuildURL = func(_ domain.Event) (string, error) { return server.URL, nil }

	event := domain.Event{
		State:       domain.StateFailure,
		RunName:     testRunID123,
		RunID:       testRunID123,
		Namespace:   testNamespaceDefault,
		Context:     testContextBuild,
		Description: testDescBuildFailed,
		TargetURL:   "https://example.com/run/123",
		CommitSHA:   "abc123",
	}

	err := n.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Notify() failed: %v", err)
	}

	if receivedPayload["routing_key"] != testKeyValue {
		t.Errorf("expected routing_key 'test-key', got %v", receivedPayload["routing_key"])
	}
	if receivedPayload["event_action"] != testActionTrigger {
		t.Errorf("expected event_action 'trigger', got %v", receivedPayload["event_action"])
	}
	if receivedPayload["dedup_key"] != testRunID123 {
		t.Errorf("expected dedup_key 'run-123', got %v", receivedPayload["dedup_key"])
	}
}

func TestNotifyWithErrorState(t *testing.T) {
	receivedPayload := make(map[string]any)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer server.Close()

	n := New(Config{IntegrationKey: testKeyValue}, nil)
	n.base.BuildURL = func(_ domain.Event) (string, error) { return server.URL, nil }

	event := domain.Event{
		State:       domain.StateError,
		RunName:     testRunID456,
		RunID:       testRunID456,
		Namespace:   "prod",
		Context:     "deploy",
		Description: "Deployment error",
	}

	err := n.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Notify() failed: %v", err)
	}

	if receivedPayload["event_action"] != testActionTrigger {
		t.Errorf("expected event_action 'trigger', got %v", receivedPayload["event_action"])
	}
}

func TestNotifyWithSuccessState(t *testing.T) {
	receivedPayload := make(map[string]any)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer server.Close()

	n := New(Config{IntegrationKey: testKeyValue}, nil)
	n.base.BuildURL = func(_ domain.Event) (string, error) { return server.URL, nil }

	event := domain.Event{
		State:       domain.StateSuccess,
		RunName:     testRunID789,
		RunID:       testRunID789,
		Namespace:   "staging",
		Context:     testContextTest,
		Description: "Tests passed",
	}

	err := n.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Notify() failed: %v", err)
	}

	if receivedPayload["event_action"] != testActionResolve {
		t.Errorf("expected event_action 'resolve', got %v", receivedPayload["event_action"])
	}
}

func TestNotifyWithIrrelevantState(t *testing.T) {
	n := New(Config{IntegrationKey: testKeyValue}, nil)

	testCases := []struct {
		name  string
		state domain.State
	}{
		{"pending", domain.StatePending},
		{"running", domain.StateRunning},
		{"canceled", domain.StateCanceled},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event := domain.Event{
				State:     tc.state,
				RunName:   "run-irrelevant",
				Namespace: testNamespaceDefault,
			}

			err := n.Handle(context.Background(), event)
			if err != nil {
				t.Errorf("Notify() should return nil for %s state, got error: %v", tc.state, err)
			}
		})
	}
}

func TestActionFor(t *testing.T) {
	testCases := []struct {
		state          domain.State
		expectedAction string
	}{
		{domain.StateFailure, testActionTrigger},
		{domain.StateError, testActionTrigger},
		{domain.StateSuccess, testActionResolve},
		{domain.StatePending, ""},
		{domain.StateRunning, ""},
		{domain.StateCanceled, ""},
	}

	for _, tc := range testCases {
		t.Run(string(tc.state), func(t *testing.T) {
			action := actionFor(tc.state)
			if action != tc.expectedAction {
				t.Errorf("actionFor(%s) = %q, want %q", tc.state, action, tc.expectedAction)
			}
		})
	}
}

func TestPayloadWithFailureState(t *testing.T) {
	n := New(Config{IntegrationKey: testKeyValue, Severity: testSeverityError}, nil)

	event := domain.Event{
		State:       domain.StateFailure,
		RunName:     testRunID123,
		RunID:       testRunID123,
		Namespace:   testNamespaceProd,
		Context:     testContextBuild,
		Description: testDescBuildFailed,
		TargetURL:   testTargetURL,
		CommitSHA:   "abc123def",
	}

	payload, err := n.payload(event)
	if err != nil {
		t.Fatalf("payload() failed: %v", err)
	}

	p, ok := payload.(map[string]any)
	if !ok {
		t.Fatal("payload is not a map")
	}

	if p["routing_key"] != testKeyValue {
		t.Errorf("expected routing_key 'test-key', got %v", p["routing_key"])
	}
	if p["event_action"] != testActionTrigger {
		t.Errorf("expected event_action 'trigger', got %v", p["event_action"])
	}
	if p["dedup_key"] != testRunID123 {
		t.Errorf("expected dedup_key 'run-123', got %v", p["dedup_key"])
	}

	innerPayload, ok := p["payload"].(map[string]any)
	if !ok {
		t.Fatal("inner payload is not a map")
	}

	if innerPayload["summary"] != "[failure] build — Build failed" {
		t.Errorf("unexpected summary: %v", innerPayload["summary"])
	}
	if innerPayload["source"] != "production/run-123" {
		t.Errorf("unexpected source: %v", innerPayload["source"])
	}
	if innerPayload["severity"] != testSeverityError {
		t.Errorf("expected severity 'error', got %v", innerPayload["severity"])
	}
	if innerPayload["component"] != testContextBuild {
		t.Errorf("expected component 'build', got %v", innerPayload["component"])
	}
	if innerPayload["group"] != testNamespaceProd {
		t.Errorf("expected group 'production', got %v", innerPayload["group"])
	}

	customDetails, ok := innerPayload["custom_details"].(map[string]string)
	if !ok {
		t.Fatal("custom_details is not a map")
	}
	if customDetails["run_id"] != testRunID123 {
		t.Errorf("expected run_id 'run-123', got %v", customDetails["run_id"])
	}
	if customDetails["namespace"] != testNamespaceProd {
		t.Errorf("expected namespace 'production', got %v", customDetails["namespace"])
	}
	if customDetails["commit_sha"] != "abc123def" {
		t.Errorf("expected commit_sha 'abc123def', got %v", customDetails["commit_sha"])
	}
	if customDetails["target_url"] != "https://tekton.example.com/run-123" {
		t.Errorf("expected target_url, got %v", customDetails["target_url"])
	}

	links, ok := p["links"].([]map[string]string)
	if !ok {
		t.Fatal("links is not a slice of maps")
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0]["href"] != "https://tekton.example.com/run-123" {
		t.Errorf("expected link href, got %v", links[0]["href"])
	}
	if links[0]["text"] != "View run" {
		t.Errorf("expected link text 'View run', got %v", links[0]["text"])
	}
}

func TestPayloadWithSuccessState(t *testing.T) {
	n := New(Config{IntegrationKey: testKeyValue}, nil)

	event := domain.Event{
		State:       domain.StateSuccess,
		RunName:     "run-success",
		Namespace:   "staging",
		Context:     testContextTest,
		Description: "All tests passed",
	}

	payload, err := n.payload(event)
	if err != nil {
		t.Fatalf("payload() failed: %v", err)
	}

	p, ok := payload.(map[string]any)
	if !ok {
		t.Fatal("payload is not a map")
	}

	if p["event_action"] != testActionResolve {
		t.Errorf("expected event_action 'resolve', got %v", p["event_action"])
	}
}

func TestPayloadWithoutTargetURL(t *testing.T) {
	n := New(Config{IntegrationKey: testKeyValue}, nil)

	event := domain.Event{
		State:       domain.StateFailure,
		RunName:     "run-no-url",
		Namespace:   testNamespaceDefault,
		Context:     testContextBuild,
		Description: testDescBuildFailed,
		TargetURL:   "",
	}

	payload, err := n.payload(event)
	if err != nil {
		t.Fatalf("payload() failed: %v", err)
	}

	p, ok := payload.(map[string]any)
	if !ok {
		t.Fatal("payload is not a map")
	}

	if _, hasLinks := p["links"]; hasLinks {
		t.Error("expected no links field when TargetURL is empty")
	}
}

func TestPayloadWithUnsupportedState(t *testing.T) {
	n := New(Config{IntegrationKey: testKeyValue}, nil)

	event := domain.Event{
		State:     domain.StatePending,
		RunName:   "run-pending",
		Namespace: testNamespaceDefault,
	}

	_, err := n.payload(event)
	if err == nil {
		t.Error("expected error for unsupported state")
	}
	expectedError := "unsupported state for pagerduty: pending"
	if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}
}

func TestNotifyHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"invalid request"}`))
	}))
	defer server.Close()

	n := New(Config{IntegrationKey: testKeyValue}, nil)
	n.base.BuildURL = func(_ domain.Event) (string, error) { return server.URL, nil }

	event := domain.Event{
		State:       domain.StateFailure,
		RunName:     "run-error",
		Namespace:   testNamespaceDefault,
		Context:     testContextBuild,
		Description: testDescBuildFailed,
	}

	err := n.Handle(context.Background(), event)
	if err == nil {
		t.Error("expected error when server returns 400")
	}
}
