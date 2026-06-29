package newrelic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	testAPIKey    = "test-insert-key"
	testAccountID = "1234567"
	testContext   = "tekton/build"
	testNamespace = "default"
	testToken     = "test"
	testRunName   = "build-123"
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
	t.Run("with all config fields", func(t *testing.T) {
		cfg := Config{
			APIKey:    scm.NewStaticToken(testAPIKey),
			AccountID: testAccountID,
		}
		n := New(cfg, nil)
		if n == nil {
			t.Fatal("expected notifier")
		}
		apiKey, err := n.cfg.APIKey.Token(context.Background())
		if err != nil {
			t.Fatalf("APIKey.Token() error = %v", err)
		}
		if apiKey != testAPIKey {
			t.Errorf("APIKey = %q, want %s", apiKey, testAPIKey)
		}
		if n.cfg.AccountID != testAccountID {
			t.Errorf("AccountID = %q, want %s", n.cfg.AccountID, testAccountID)
		}
		if n.base == nil {
			t.Error("base notifier should be initialized")
		}
	})

	t.Run("with empty config", func(t *testing.T) {
		n := New(Config{}, nil)
		if n == nil {
			t.Fatal("expected notifier even with empty config")
		}
		if n.cfg.AccountID != "" {
			t.Errorf("AccountID = %q, want empty", n.cfg.AccountID)
		}
	})
}

func TestName(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken(testToken)}, nil)
	if n.Name() != notifierName {
		t.Errorf("Name() = %q, want %s", n.Name(), notifierName)
	}
}

func TestNotifier_ResolvesAPIKeyPerRequest(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("X-Insert-Key"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := New(Config{APIKey: &rotatingToken{values: []string{"v1", "v2"}}, AccountID: testAccountID}, nil)
	n.base.HTTP = server.Client()
	n.base.BuildURL = func(_ domain.Event) (string, error) { return server.URL, nil }

	event := domain.Event{State: domain.StateSuccess, Context: testContext, Namespace: testNamespace, RunName: "run"}
	if err := n.Handle(context.Background(), event); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	if err := n.Handle(context.Background(), event); err != nil {
		t.Fatalf("second Handle() error = %v", err)
	}
	if len(got) != 2 || got[0] != "v1" || got[1] != "v2" {
		t.Fatalf("X-Insert-Key headers = %v, want [v1 v2]", got)
	}
}

func TestNotify(t *testing.T) {
	t.Run("successful notification", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("Method = %s, want POST", r.Method)
			}
			if r.Header.Get("X-Insert-Key") != testAPIKey {
				t.Errorf("X-Insert-Key header = %q, want %s", r.Header.Get("X-Insert-Key"), testAPIKey)
			}
			if r.Header.Get("User-Agent") != notifier.UserAgent {
				t.Errorf("User-Agent = %q, want %s", r.Header.Get("User-Agent"), notifier.UserAgent)
			}

			body, _ := io.ReadAll(r.Body)
			var events []map[string]any
			if err := json.Unmarshal(body, &events); err != nil {
				t.Errorf("failed to unmarshal payload: %v", err)
			}
			if len(events) != 1 {
				t.Errorf("events length = %d, want 1", len(events))
			}

			event := events[0]
			if event["eventType"] != eventType {
				t.Errorf("eventType = %v, want %s", event["eventType"], eventType)
			}
			if event["runName"] != testRunName {
				t.Errorf("runName = %v, want build-123", event["runName"])
			}
			if event["state"] != string(domain.StateSuccess) {
				t.Errorf("state = %v, want %s", event["state"], domain.StateSuccess)
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := Config{
			APIKey:    scm.NewStaticToken(testAPIKey),
			AccountID: testAccountID,
		}
		n := New(cfg, nil)
		n.base.HTTP = server.Client()
		n.base.BuildURL = func(_ domain.Event) (string, error) {
			return server.URL, nil
		}

		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     testContext,
			Description: "Build succeeded",
			Namespace:   testNamespace,
			RunID:       testRunName,
			RunName:     testRunName,
			Resource:    domain.ResourcePipelineRun,
			CommitSHA:   "abc123def456",
		}

		err := n.Handle(context.Background(), event)
		if err != nil {
			t.Errorf("Handle() error = %v, want nil", err)
		}
	})

	t.Run("notification with server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
		}))
		defer server.Close()

		n := New(Config{APIKey: scm.NewStaticToken(testAPIKey), AccountID: testAccountID}, nil)
		n.base.HTTP = server.Client()
		n.base.BuildURL = func(_ domain.Event) (string, error) {
			return server.URL, nil
		}

		event := domain.Event{
			State:       domain.StateFailure,
			Context:     "tekton/test",
			Description: "Tests failed",
			Namespace:   testNamespace,
			RunName:     "test-456",
			Resource:    domain.ResourceTaskRun,
		}

		err := n.Handle(context.Background(), event)
		if err == nil {
			t.Error("Handle() error = nil, want error")
		}
	})

	t.Run("sends all states - filtering done externally by CEL", func(t *testing.T) {
		serverCalled := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			serverCalled = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := Config{
			APIKey:    scm.NewStaticToken(testAPIKey),
			AccountID: testAccountID,
		}
		n := New(cfg, nil)
		n.base.HTTP = server.Client()
		n.base.BuildURL = func(_ domain.Event) (string, error) {
			return server.URL, nil
		}

		event := domain.Event{
			State:       domain.StateRunning,
			Context:     testContext,
			Description: "Build running",
			Namespace:   testNamespace,
			RunName:     "build-789",
			Resource:    domain.ResourcePipelineRun,
		}

		err := n.Handle(context.Background(), event)
		if err != nil {
			t.Errorf("Handle() error = %v, want nil", err)
		}
		if !serverCalled {
			t.Error("server should have been called - Handle sends unconditionally")
		}
	})
}

func TestPayload(t *testing.T) {
	t.Run("with all fields", func(t *testing.T) {
		n := New(Config{APIKey: scm.NewStaticToken(testAPIKey), AccountID: testAccountID}, nil)
		event := domain.Event{
			State:        domain.StateSuccess,
			Context:      testContext,
			Description:  "Build succeeded",
			Namespace:    testNamespace,
			RunID:        testRunName,
			RunName:      testRunName,
			Resource:     domain.ResourcePipelineRun,
			CommitSHA:    "abc123def456",
			Provider:     "github",
			PipelineName: "my-pipeline",
			TaskName:     "my-task",
		}

		payload, err := n.payload(event)
		if err != nil {
			t.Fatalf("payload() error = %v, want nil", err)
		}

		events, ok := payload.([]any)
		if !ok {
			t.Fatal("payload is not []any")
		}
		if len(events) != 1 {
			t.Fatalf("events length = %d, want 1", len(events))
		}

		e, ok := events[0].(map[string]any)
		if !ok {
			t.Fatal("event is not map[string]any")
		}

		if e["eventType"] != eventType {
			t.Errorf("eventType = %v, want %s", e["eventType"], eventType)
		}
		if e["runName"] != testRunName {
			t.Errorf("runName = %v, want build-123", e["runName"])
		}
		if e["state"] != string(domain.StateSuccess) {
			t.Errorf("state = %v, want %s", e["state"], domain.StateSuccess)
		}
		if e["namespace"] != testNamespace {
			t.Errorf("namespace = %v, want %s", e["namespace"], testNamespace)
		}
		if e["runID"] != testRunName {
			t.Errorf("runID = %v, want build-123", e["runID"])
		}
		if e["resource"] != string(domain.ResourcePipelineRun) {
			t.Errorf("resource = %v, want %s", e["resource"], domain.ResourcePipelineRun)
		}
		if e["provider"] != "github" {
			t.Errorf("provider = %v, want github", e["provider"])
		}
		if e["context"] != testContext {
			t.Errorf("context = %v, want %s", e["context"], testContext)
		}
		if e["description"] != "Build succeeded" {
			t.Errorf("description = %v, want Build succeeded", e["description"])
		}
		if e["commitSHA"] != "abc123def456" {
			t.Errorf("commitSHA = %v, want abc123def456", e["commitSHA"])
		}
		if e["pipelineName"] != "my-pipeline" {
			t.Errorf("pipelineName = %v, want my-pipeline", e["pipelineName"])
		}
		if e["taskName"] != "my-task" {
			t.Errorf("taskName = %v, want my-task", e["taskName"])
		}
	})

	t.Run("with minimal fields", func(t *testing.T) {
		n := New(Config{APIKey: scm.NewStaticToken(testAPIKey), AccountID: testAccountID}, nil)
		event := domain.Event{
			State:     domain.StateRunning,
			Namespace: testNamespace,
			RunName:   "run-1",
			Resource:  domain.ResourceTaskRun,
		}

		payload, err := n.payload(event)
		if err != nil {
			t.Fatalf("payload() error = %v, want nil", err)
		}

		events := payload.([]any)
		e := events[0].(map[string]any)

		if e["eventType"] != eventType {
			t.Errorf("eventType = %v, want %s", e["eventType"], eventType)
		}
		if _, ok := e["provider"]; ok {
			t.Error("provider should not be present when empty")
		}
		if _, ok := e["context"]; ok {
			t.Error("context should not be present when empty")
		}
		if _, ok := e["description"]; ok {
			t.Error("description should not be present when empty")
		}
		if _, ok := e["commitSHA"]; ok {
			t.Error("commitSHA should not be present when empty")
		}
		if _, ok := e["pipelineName"]; ok {
			t.Error("pipelineName should not be present when empty")
		}
		if _, ok := e["taskName"]; ok {
			t.Error("taskName should not be present when empty")
		}
	})
}

func TestAuth(t *testing.T) {
	t.Run("with valid api key", func(t *testing.T) {
		n := New(Config{APIKey: scm.NewStaticToken("secret-insert-key"), AccountID: testAccountID}, nil)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, defaultInsightsURL, nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		if err := n.auth(req); err != nil {
			t.Fatalf("auth() unexpected error: %v", err)
		}

		apiKey := req.Header.Get("X-Insert-Key")
		if apiKey != "secret-insert-key" {
			t.Errorf("X-Insert-Key header = %q, want secret-insert-key", apiKey)
		}
	})

	t.Run("with nil api key refresher", func(t *testing.T) {
		n := New(Config{AccountID: testAccountID}, nil)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, defaultInsightsURL, nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		if err := n.auth(req); err == nil {
			t.Error("auth() error = nil, want error")
		}
	})
}

func TestURL(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		expected  string
	}{
		{
			name:      "standard account",
			accountID: "1234567",
			expected:  "https://insights-collector.newrelic.com/v1/accounts/1234567/events",
		},
		{
			name:      "different account",
			accountID: "9999999",
			expected:  "https://insights-collector.newrelic.com/v1/accounts/9999999/events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := New(Config{
				APIKey:    scm.NewStaticToken(testAPIKey),
				AccountID: tt.accountID,
			}, nil)

			url, err := n.url(domain.Event{})
			if err != nil {
				t.Errorf("url() error = %v, want nil", err)
			}
			if url != tt.expected {
				t.Errorf("url() = %s, want %s", url, tt.expected)
			}
		})
	}
}

func TestClose(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken(testAPIKey), AccountID: testAccountID}, nil)
	if err := n.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}
