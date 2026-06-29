package honeycomb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	testAPIKey    = "test-key"
	testDataset   = "tekton-events"
	testNamespace = "default"
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
			APIKey:  scm.NewStaticToken(testAPIKey),
			Dataset: testDataset,
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
		if n.cfg.Dataset != testDataset {
			t.Errorf("Dataset = %q, want %s", n.cfg.Dataset, testDataset)
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
	})
}

func TestName(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken("test")}, nil)
	if n.Name() != notifierName {
		t.Errorf("Name() = %q, want %s", n.Name(), notifierName)
	}
}

func TestType(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken("test")}, nil)
	if n.Type() != notifier.ActionNotify {
		t.Errorf("Type() = %v, want %v", n.Type(), notifier.ActionNotify)
	}
}

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name     string
		dataset  string
		expected string
	}{
		{
			name:     "standard dataset",
			dataset:  "tekton-events",
			expected: "https://api.honeycomb.io/1/events/tekton-events",
		},
		{
			name:     "dataset with hyphens",
			dataset:  "my-ci-events",
			expected: "https://api.honeycomb.io/1/events/my-ci-events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := New(Config{
				APIKey:  scm.NewStaticToken(testAPIKey),
				Dataset: tt.dataset,
			}, nil)

			url, err := n.buildURL(domain.Event{})
			if err != nil {
				t.Errorf("buildURL() error = %v, want nil", err)
			}
			if url != tt.expected {
				t.Errorf("buildURL() = %s, want %s", url, tt.expected)
			}
		})
	}
}

func TestAuth(t *testing.T) {
	t.Run("with valid api key", func(t *testing.T) {
		n := New(Config{APIKey: scm.NewStaticToken("secret-api-key")}, nil)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.honeycomb.io/1/events/test", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		if err := n.auth(req); err != nil {
			t.Fatalf("auth() unexpected error: %v", err)
		}

		apiKey := req.Header.Get("X-Honeycomb-Team")
		if apiKey != "secret-api-key" {
			t.Errorf("X-Honeycomb-Team header = %q, want secret-api-key", apiKey)
		}
	})

	t.Run("with nil api key refresher", func(t *testing.T) {
		n := New(Config{}, nil)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.honeycomb.io/1/events/test", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		if err := n.auth(req); err == nil {
			t.Error("auth() expected error for nil api key refresher")
		}
	})
}

func TestNotifier_ResolvesAPIKeyPerRequest(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("X-Honeycomb-Team"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := New(Config{
		APIKey:  &rotatingToken{values: []string{"v1", "v2"}},
		Dataset: testDataset,
	}, nil)
	n.base.HTTP = server.Client()
	n.base.BuildURL = func(_ domain.Event) (string, error) { return server.URL, nil }

	event := domain.Event{State: domain.StateSuccess, Context: "test", Namespace: testNamespace, RunName: "run"}
	if err := n.Handle(context.Background(), event); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	if err := n.Handle(context.Background(), event); err != nil {
		t.Fatalf("second Handle() error = %v", err)
	}
	if len(got) != 2 || got[0] != "v1" || got[1] != "v2" {
		t.Fatalf("X-Honeycomb-Team headers = %v, want [v1 v2]", got)
	}
}

func TestHandle(t *testing.T) {
	t.Run("successful notification", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("Method = %s, want POST", r.Method)
			}
			if r.Header.Get("X-Honeycomb-Team") != testAPIKey {
				t.Errorf("X-Honeycomb-Team header = %q, want %s", r.Header.Get("X-Honeycomb-Team"), testAPIKey)
			}
			if r.Header.Get("User-Agent") != notifier.UserAgent {
				t.Errorf("User-Agent = %q, want %s", r.Header.Get("User-Agent"), notifier.UserAgent)
			}

			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("failed to unmarshal payload: %v", err)
			}

			if payload["runName"] != testRunName {
				t.Errorf("runName = %v, want build-123", payload["runName"])
			}
			if payload["state"] != "success" {
				t.Errorf("state = %v, want success", payload["state"])
			}
			if payload["namespace"] != testNamespace {
				t.Errorf("namespace = %v, want %s", payload["namespace"], testNamespace)
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := Config{
			APIKey:  scm.NewStaticToken(testAPIKey),
			Dataset: testDataset,
		}
		n := New(cfg, nil)
		n.base.HTTP = server.Client()
		n.base.BuildURL = func(_ domain.Event) (string, error) {
			return server.URL, nil
		}

		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     "tekton/build",
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

		n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)
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

	t.Run("sends all states unconditionally", func(t *testing.T) {
		serverCalled := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			serverCalled = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		n := New(Config{APIKey: scm.NewStaticToken(testAPIKey)}, nil)
		n.base.HTTP = server.Client()
		n.base.BuildURL = func(_ domain.Event) (string, error) {
			return server.URL, nil
		}

		event := domain.Event{
			State:       domain.StateRunning,
			Context:     "tekton/build",
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
			t.Error("server should have been called")
		}
	})
}

func TestBuildPayload(t *testing.T) {
	t.Run("with commit SHA and times", func(t *testing.T) {
		n := New(Config{
			APIKey:  scm.NewStaticToken(testAPIKey),
			Dataset: testDataset,
		}, nil)

		started := parseTime(t, "2024-01-15T10:00:00Z")
		finished := parseTime(t, "2024-01-15T10:05:00Z")

		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     "tekton/build",
			Description: "Build succeeded",
			Namespace:   testNamespace,
			RunID:       testRunName,
			RunName:     testRunName,
			Resource:    domain.ResourcePipelineRun,
			CommitSHA:   "abc123def456",
			StartedAt:   started,
			FinishedAt:  finished,
			TargetURL:   "https://dashboard.example.com/run/build-123",
		}

		payload, err := n.buildPayload(event)
		if err != nil {
			t.Fatalf("buildPayload() error = %v, want nil", err)
		}

		p, ok := payload.(map[string]any)
		if !ok {
			t.Fatal("payload is not map[string]any")
		}

		if p["runName"] != testRunName {
			t.Errorf("runName = %v, want build-123", p["runName"])
		}
		if p["state"] != "success" {
			t.Errorf("state = %v, want success", p["state"])
		}
		if p["provider"] != "" {
			t.Errorf("provider = %v, want empty", p["provider"])
		}
		if p["namespace"] != testNamespace {
			t.Errorf("namespace = %v, want %s", p["namespace"], testNamespace)
		}
		if p["runID"] != testRunName {
			t.Errorf("runID = %v, want build-123", p["runID"])
		}
		if p["commitSHA"] != "abc123def456" {
			t.Errorf("commitSHA = %v, want abc123def456", p["commitSHA"])
		}
		if p["startedAt"] != "2024-01-15T10:00:00Z" {
			t.Errorf("startedAt = %v, want 2024-01-15T10:00:00Z", p["startedAt"])
		}
		if p["finishedAt"] != "2024-01-15T10:05:00Z" {
			t.Errorf("finishedAt = %v, want 2024-01-15T10:05:00Z", p["finishedAt"])
		}
		if p["targetURL"] != "https://dashboard.example.com/run/build-123" {
			t.Errorf("targetURL = %v, want https://dashboard.example.com/run/build-123", p["targetURL"])
		}
		if p["userAgent"] != notifier.UserAgent {
			t.Errorf("userAgent = %v, want %s", p["userAgent"], notifier.UserAgent)
		}
	})

	t.Run("without optional fields", func(t *testing.T) {
		n := New(Config{
			APIKey:  scm.NewStaticToken(testAPIKey),
			Dataset: testDataset,
		}, nil)

		event := domain.Event{
			State:     domain.StateFailure,
			Namespace: testNamespace,
			RunName:   "test-456",
			Resource:  domain.ResourceTaskRun,
		}

		payload, err := n.buildPayload(event)
		if err != nil {
			t.Fatalf("buildPayload() error = %v, want nil", err)
		}

		p, ok := payload.(map[string]any)
		if !ok {
			t.Fatal("payload is not map[string]any")
		}

		if _, exists := p["commitSHA"]; exists {
			t.Error("commitSHA should not be present when empty")
		}
		if _, exists := p["startedAt"]; exists {
			t.Error("startedAt should not be present when zero")
		}
		if _, exists := p["finishedAt"]; exists {
			t.Error("finishedAt should not be present when zero")
		}
		if _, exists := p["targetURL"]; exists {
			t.Error("targetURL should not be present when empty")
		}
	})
}

func TestClose(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken("test")}, nil)
	if err := n.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func parseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return ts
}
