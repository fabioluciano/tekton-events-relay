package opsgenie

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	testAPIKey           = "test-api-key"
	testNameOpsgenie     = "opsgenie"
	testRunID123         = "run-123"
	testRunID456         = "run-456"
	testNamespaceDefault = "default"
	testNamespaceProd    = "production"
	testContextBuild     = "build"
	testDescBuildFailed  = "Build failed"
)

type rotatingToken struct {
	values []string
	index  int
}

func (r *rotatingToken) Token(context.Context) (string, error) {
	if r.index >= len(r.values) {
		return r.values[len(r.values)-1], nil
	}
	value := r.values[r.index]
	r.index++
	return value, nil
}

func TestNew(t *testing.T) {
	cfg := Config{APIKey: scm.NewStaticToken(testAPIKey)}
	n := New(cfg, nil)
	if n == nil {
		t.Fatal("expected notifier")
	}
}

func TestNewWithDefaultPriority(t *testing.T) {
	cfg := Config{APIKey: scm.NewStaticToken(testAPIKey)}
	n := New(cfg, nil)
	if n.cfg.Priority != defaultPriority {
		t.Errorf("expected default priority %q, got %q", defaultPriority, n.cfg.Priority)
	}
}

func TestNewWithCustomPriority(t *testing.T) {
	cfg := Config{APIKey: scm.NewStaticToken(testAPIKey), Priority: "P1"}
	n := New(cfg, nil)
	if n.cfg.Priority != "P1" {
		t.Errorf("expected priority 'P1', got %q", n.cfg.Priority)
	}
}

func TestName(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)
	if n.Name() != testNameOpsgenie {
		t.Errorf("Name() = %q, want opsgenie", n.Name())
	}
}

func TestType(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)
	if n.Type() != "notify" {
		t.Errorf("Type() = %q, want notify", n.Type())
	}
}

func TestClose(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)
	if err := n.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestNotifier_ResolvesAPIKeyPerRequest(t *testing.T) {
	var gotKeys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeys = append(gotKeys, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer server.Close()

	n := New(Config{APIKey: &rotatingToken{values: []string{"v1", "v2"}}}, nil)
	n.base.BuildURL = func(_ domain.Event) (string, error) { return server.URL, nil }

	event := domain.Event{
		State:     domain.StateFailure,
		RunName:   testRunID123,
		RunID:     testRunID123,
		Namespace: testNamespaceDefault,
		Context:   testContextBuild,
	}

	if err := n.Handle(context.Background(), event); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	if err := n.Handle(context.Background(), event); err != nil {
		t.Fatalf("second Handle() error = %v", err)
	}
	if len(gotKeys) != 2 || gotKeys[0] != "GenieKey v1" || gotKeys[1] != "GenieKey v2" {
		t.Fatalf("Authorization values = %v, want [GenieKey v1 GenieKey v2]", gotKeys)
	}
}

func TestNotifyWithFailureState(t *testing.T) {
	var receivedPayload map[string]any
	var receivedMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer server.Close()

	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)
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
		t.Fatalf("Handle() failed: %v", err)
	}

	if receivedMethod != http.MethodPost {
		t.Errorf("expected method POST, got %s", receivedMethod)
	}
	if receivedPayload["message"] != "[failure] build — Build failed" {
		t.Errorf("unexpected message: %v", receivedPayload["message"])
	}
	if receivedPayload["alias"] != "tekton-relay:run-123" {
		t.Errorf("expected alias 'tekton-relay:run-123', got %v", receivedPayload["alias"])
	}
	if receivedPayload["priority"] != defaultPriority {
		t.Errorf("expected priority %q, got %v", defaultPriority, receivedPayload["priority"])
	}
	if receivedPayload["source"] != "default/run-123" {
		t.Errorf("expected source 'default/run-123', got %v", receivedPayload["source"])
	}
}

func TestNotifyWithErrorState(t *testing.T) {
	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer server.Close()

	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)
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
		t.Fatalf("Handle() failed: %v", err)
	}

	if receivedPayload["alias"] != "tekton-relay:run-456" {
		t.Errorf("expected alias 'tekton-relay:run-456', got %v", receivedPayload["alias"])
	}
}

func TestNotifyWithSuccessState(t *testing.T) {
	var receivedMethod string
	var receivedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer server.Close()

	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)
	n.base.BuildURL = func(_ domain.Event) (string, error) { return server.URL + "/v2/alerts", nil }

	event := domain.Event{
		State:       domain.StateSuccess,
		RunName:     "run-789",
		RunID:       "run-789",
		Namespace:   "staging",
		Context:     "test",
		Description: "Tests passed",
	}

	err := n.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	if receivedMethod != http.MethodDelete {
		t.Errorf("expected method DELETE, got %s", receivedMethod)
	}
	if !strings.Contains(receivedURL, "tekton-relay:run-789") {
		t.Errorf("expected URL to contain alias, got %s", receivedURL)
	}
	if !strings.Contains(receivedURL, "identifierType=alias") {
		t.Errorf("expected URL to contain identifierType=alias, got %s", receivedURL)
	}
}

func TestNotifyWithIrrelevantState(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)

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
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			n.base.BuildURL = func(_ domain.Event) (string, error) { return server.URL, nil }

			event := domain.Event{
				State:     tc.state,
				RunName:   "run-irrelevant",
				Namespace: testNamespaceDefault,
			}

			err := n.Handle(context.Background(), event)
			if err != nil {
				t.Errorf("Handle() should return nil for %s state, got error: %v", tc.state, err)
			}
			if called {
				t.Errorf("expected no HTTP call for %s state", tc.state)
			}
		})
	}
}

func TestActionFor(t *testing.T) {
	testCases := []struct {
		name           string
		state          domain.State
		expectedAction string
	}{
		{"failure", domain.StateFailure, actionCreate},
		{"error", domain.StateError, actionCreate},
		{"success", domain.StateSuccess, actionClose},
		{"pending", domain.StatePending, ""},
		{"running", domain.StateRunning, ""},
		{"canceled", domain.StateCanceled, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			action := actionFor(tc.state)
			if action != tc.expectedAction {
				t.Errorf("actionFor(%s) = %q, want %q", tc.state, action, tc.expectedAction)
			}
		})
	}
}

func TestPayloadWithTeamName(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey), TeamName: "platform-team"}, nil)

	event := domain.Event{
		State:       domain.StateFailure,
		RunName:     testRunID123,
		RunID:       testRunID123,
		Namespace:   testNamespaceProd,
		Context:     testContextBuild,
		Description: testDescBuildFailed,
		TargetURL:   "https://tekton.example.com/run-123",
		CommitSHA:   "abc123def",
	}

	payload, err := n.payload(event, testAPIKey)
	if err != nil {
		t.Fatalf("payload() failed: %v", err)
	}

	p, ok := payload.(map[string]any)
	if !ok {
		t.Fatal("payload is not a map")
	}

	responders, ok := p["responders"].([]map[string]string)
	if !ok {
		t.Fatal("responders is not a slice of maps")
	}
	if len(responders) != 1 {
		t.Fatalf("expected 1 responder, got %d", len(responders))
	}
	if responders[0]["type"] != "team" || responders[0]["name"] != "platform-team" {
		t.Errorf("unexpected responder: %v", responders[0])
	}
}

func TestPayloadWithoutTeamName(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)

	event := domain.Event{
		State:     domain.StateFailure,
		RunName:   testRunID123,
		RunID:     testRunID123,
		Namespace: testNamespaceDefault,
		Context:   testContextBuild,
	}

	payload, err := n.payload(event, testAPIKey)
	if err != nil {
		t.Fatalf("payload() failed: %v", err)
	}

	p, ok := payload.(map[string]any)
	if !ok {
		t.Fatal("payload is not a map")
	}

	if _, hasResponders := p["responders"]; hasResponders {
		t.Error("expected no responders when TeamName is empty")
	}
}

func TestPayloadWithTargetURL(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)

	event := domain.Event{
		State:     domain.StateFailure,
		RunName:   testRunID123,
		RunID:     testRunID123,
		Namespace: testNamespaceDefault,
		Context:   testContextBuild,
		TargetURL: "https://example.com/run",
	}

	payload, err := n.payload(event, testAPIKey)
	if err != nil {
		t.Fatalf("payload() failed: %v", err)
	}

	p, ok := payload.(map[string]any)
	if !ok {
		t.Fatal("payload is not a map")
	}

	actions, ok := p["actions"].([]string)
	if !ok {
		t.Fatal("actions is not a string slice")
	}
	if len(actions) != 1 || actions[0] != "View Run" {
		t.Errorf("unexpected actions: %v", actions)
	}
}

func TestPayloadWithoutTargetURL(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)

	event := domain.Event{
		State:     domain.StateFailure,
		RunName:   testRunID123,
		RunID:     testRunID123,
		Namespace: testNamespaceDefault,
		Context:   testContextBuild,
	}

	payload, err := n.payload(event, testAPIKey)
	if err != nil {
		t.Fatalf("payload() failed: %v", err)
	}

	p, ok := payload.(map[string]any)
	if !ok {
		t.Fatal("payload is not a map")
	}

	if _, hasActions := p["actions"]; hasActions {
		t.Error("expected no actions when TargetURL is empty")
	}
}

func TestNotifyHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"invalid request"}`))
	}))
	defer server.Close()

	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)
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

func TestHandleNilAPIKey(t *testing.T) {
	n := New(Config{}, nil)

	event := domain.Event{
		State:   domain.StateFailure,
		RunName: testRunID123,
		RunID:   testRunID123,
	}

	err := n.Handle(context.Background(), event)
	if err == nil {
		t.Error("expected error when API key is nil")
	}
}
