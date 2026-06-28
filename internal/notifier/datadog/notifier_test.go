package datadog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	testAPIKey       = "test-key"
	testSiteEU       = "datadoghq.eu"
	testEnvProd      = "env:prod"
	testFailure      = "failure"
	testContext      = "tekton/build"
	testTeamPlatform = "team:platform"
	testAPIKeyValue  = "testAPIKey"
	testSiteValue    = "testSiteEU"
	testContextValue = "testContext"
	testNamespace    = "default"
	testToken        = "test"
	stateError       = "error"
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
			APIKey: scm.NewStaticToken(testAPIKeyValue),
			Site:   testSiteValue,
			Tags:   []string{testEnvProd, testTeamPlatform},
		}
		n := New(cfg, nil)
		if n == nil {
			t.Fatal("expected notifier")
		}
		apiKey, err := n.cfg.APIKey.Token(context.Background())
		if err != nil {
			t.Fatalf("APIKey.Token() error = %v", err)
		}
		if apiKey != testAPIKeyValue {
			t.Errorf("APIKey = %q, want %s", apiKey, testAPIKeyValue)
		}
		if n.cfg.Site != testSiteValue {
			t.Errorf("Site = %q, want %s", n.cfg.Site, testSiteValue)
		}
		if len(n.cfg.Tags) != 2 {
			t.Errorf("Tags length = %d, want 2", len(n.cfg.Tags))
		}
		if n.base == nil {
			t.Error("base notifier should be initialized")
		}
	})

	t.Run("with default site", func(t *testing.T) {
		cfg := Config{APIKey: scm.NewStaticToken(testAPIKeyValue)}
		n := New(cfg, nil)
		if n.cfg.Site != defaultSite {
			t.Errorf("Site = %q, want datadoghq.com (default)", n.cfg.Site)
		}
	})

	t.Run("with empty config", func(t *testing.T) {
		n := New(Config{}, nil)
		if n == nil {
			t.Fatal("expected notifier even with empty config")
		}
		if n.cfg.Site != defaultSite {
			t.Errorf("Site = %q, want datadoghq.com (default)", n.cfg.Site)
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
		got = append(got, r.Header.Get("DD-API-KEY"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := New(Config{APIKey: &rotatingToken{values: []string{"v1", "v2"}}}, nil)
	n.base.HTTP = server.Client()
	n.base.BuildURL = func(_ domain.Event) (string, error) { return server.URL, nil }

	event := domain.Event{State: domain.StateSuccess, Context: testContextValue, Namespace: testNamespace, RunName: "run"}
	if err := n.Handle(context.Background(), event); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	if err := n.Handle(context.Background(), event); err != nil {
		t.Fatalf("second Handle() error = %v", err)
	}
	if len(got) != 2 || got[0] != "v1" || got[1] != "v2" {
		t.Fatalf("DD-API-KEY headers = %v, want [v1 v2]", got)
	}
}

func TestNotify(t *testing.T) {
	t.Run("successful notification", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("Method = %s, want POST", r.Method)
			}
			if r.Header.Get("DD-API-KEY") != testAPIKeyValue {
				t.Errorf("DD-API-KEY header = %q, want %s", r.Header.Get("DD-API-KEY"), testAPIKeyValue)
			}
			if r.Header.Get("User-Agent") != notifier.UserAgent {
				t.Errorf("User-Agent = %q, want %s", r.Header.Get("User-Agent"), notifier.UserAgent)
			}

			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("failed to unmarshal payload: %v", err)
			}

			if payload["source_type_name"] != notifier.UserAgent {
				t.Errorf("source_type_name = %v, want %s", payload["source_type_name"], notifier.UserAgent)
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := Config{
			APIKey: scm.NewStaticToken(testAPIKeyValue),
			Site:   strings.TrimPrefix(server.URL, "https://api."),
		}
		n := New(cfg, nil)
		n.base.HTTP = server.Client()
		n.cfg.Site = strings.TrimPrefix(server.URL, "https://api.")

		// Override url builder to use test server
		n.base.BuildURL = func(_ domain.Event) (string, error) {
			return server.URL, nil
		}

		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     testContextValue,
			Description: "Build succeeded",
			Namespace:   testNamespace,
			RunID:       "build-123",

			RunName:   "build-123",
			Resource:  domain.ResourcePipelineRun,
			CommitSHA: "abc123def456",
		}

		err := n.Handle(context.Background(), event)
		if err != nil {
			t.Errorf("Notify() error = %v, want nil", err)
		}
	})

	t.Run("notification with server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
		}))
		defer server.Close()

		n := New(Config{APIKey: scm.NewStaticToken(testAPIKeyValue)}, nil)
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
			t.Error("Notify() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "after") || !strings.Contains(err.Error(), "attempt") {
			t.Errorf("error = %v, want error containing retryable error pattern", err)
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
			APIKey: scm.NewStaticToken(testAPIKeyValue),
		}
		n := New(cfg, nil)
		n.base.HTTP = server.Client()
		n.base.BuildURL = func(_ domain.Event) (string, error) {
			return server.URL, nil
		}

		event := domain.Event{
			State:       domain.StateRunning,
			Context:     testContextValue,
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

	t.Run("no filter - sends all states", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		n := New(Config{APIKey: scm.NewStaticToken(testAPIKeyValue)}, nil)
		n.base.HTTP = server.Client()
		n.base.BuildURL = func(_ domain.Event) (string, error) {
			return server.URL, nil
		}

		event := domain.Event{
			State:       domain.StateRunning,
			Context:     testContextValue,
			Description: "Build running",
			Namespace:   testNamespace,
			RunName:     "build-999",
			Resource:    domain.ResourcePipelineRun,
		}

		err := n.Handle(context.Background(), event)
		if err != nil {
			t.Errorf("Notify() error = %v, want nil", err)
		}
	})
}

func newTestDatadogNotifier(t *testing.T) *Notifier {
	t.Helper()
	return New(Config{
		APIKey: scm.NewStaticToken(testAPIKeyValue),
		Tags:   []string{testEnvProd, testTeamPlatform},
	}, nil)
}

func verifyDDTagPresent(t *testing.T, tags []string, expected string) {
	t.Helper()
	for _, tag := range tags {
		if tag == expected {
			return
		}
	}
	t.Errorf("expected tag %q not found in %v", expected, tags)
}

func TestPayload_WithCommitSHA(t *testing.T) {
	n := newTestDatadogNotifier(t)
	event := domain.Event{
		State:       domain.StateSuccess,
		Context:     testContext,
		Description: "Build succeeded",
		Namespace:   testNamespace,
		RunName:     "build-123",
		Resource:    domain.ResourcePipelineRun,
		CommitSHA:   "abc123def456789",
	}

	payload, err := n.payload(event)
	if err != nil {
		t.Fatalf("payload() error = %v, want nil", err)
	}

	p, ok := payload.(map[string]any)
	if !ok {
		t.Fatal("payload is not map[string]any")
	}

	if p["title"] != "[tekton-events-relay] "+testContext+" — success" {
		t.Errorf("title = %v, want [tekton-events-relay] %s — success", p["title"], testContext)
	}
	if !strings.Contains(p["text"].(string), "Build succeeded") {
		t.Errorf("text does not contain description")
	}
	if !strings.Contains(p["text"].(string), "default/build-123") {
		t.Errorf("text does not contain run info")
	}
	if p["alert_type"] != alertSuccess {
		t.Errorf("alert_type = %v, want %s", p["alert_type"], alertSuccess)
	}
	if p["source_type_name"] != notifier.UserAgent {
		t.Errorf("source_type_name = %v, want %s", p["source_type_name"], notifier.UserAgent)
	}

	tags, ok := p["tags"].([]string)
	if !ok {
		t.Fatal("tags is not []string")
	}

	expectedTags := []string{
		"state:success",
		"context:tekton_build",
		"namespace:default",
		"run_id:build-123",
		"resource:pipelinerun",
		"commit_sha:abc123d",
		testEnvProd,
		testTeamPlatform,
	}

	if len(tags) != len(expectedTags) {
		t.Errorf("tags length = %d, want %d", len(tags), len(expectedTags))
	}

	for _, expected := range expectedTags {
		verifyDDTagPresent(t, tags, expected)
	}
}

func TestPayload_WithoutCommitSHA(t *testing.T) {
	n := newTestDatadogNotifier(t)
	event := domain.Event{
		State:       domain.StateFailure,
		Context:     "tekton/test",
		Description: "Tests failed",
		Namespace:   testNamespace,
		RunName:     "test-456",
		Resource:    domain.ResourceTaskRun,
		CommitSHA:   "",
	}

	payload, err := n.payload(event)
	if err != nil {
		t.Fatalf("payload() error = %v, want nil", err)
	}

	p, ok := payload.(map[string]any)
	if !ok {
		t.Fatal("payload is not map[string]any")
	}

	tags := p["tags"].([]string)
	hasCommitTag := false
	for _, tag := range tags {
		if strings.HasPrefix(tag, "commit_sha:") {
			hasCommitTag = true
			break
		}
	}
	if hasCommitTag {
		t.Error("commit_sha tag should not be present when CommitSHA is empty")
	}
}

func TestPayload_WithShortCommitSHA(t *testing.T) {
	n := newTestDatadogNotifier(t)
	event := domain.Event{
		State:       domain.StateSuccess,
		Context:     testToken,
		Description: testToken,
		Namespace:   testNamespace,
		RunName:     "run-1",
		Resource:    domain.ResourceTaskRun,
		CommitSHA:   "abc",
	}

	payload, err := n.payload(event)
	if err != nil {
		t.Fatalf("payload() error = %v, want nil", err)
	}

	p := payload.(map[string]any)
	tags := p["tags"].([]string)

	found := false
	for _, tag := range tags {
		if tag == "commit_sha:abc" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected commit_sha:abc in tags")
	}
}

func TestPayload_SanitizesContextWithSlashesAndColons(t *testing.T) {
	n := newTestDatadogNotifier(t)
	event := domain.Event{
		State:       domain.StateSuccess,
		Context:     "ci/cd:build/test",
		Description: "test",
		Namespace:   testNamespace,
		RunName:     "run-1",
		Resource:    domain.ResourceTaskRun,
	}

	payload, err := n.payload(event)
	if err != nil {
		t.Fatalf("payload() error = %v, want nil", err)
	}

	p := payload.(map[string]any)
	tags := p["tags"].([]string)

	found := false
	for _, tag := range tags {
		if tag == "context:ci_cd_build_test" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected sanitized context tag, got %v", tags)
	}
}

func TestAuth(t *testing.T) {
	n := New(Config{APIKey: scm.NewStaticToken("secret-api-key")}, nil)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.datadoghq.com/api/v2/events", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	if err := n.auth(req); err != nil {
		t.Fatalf("auth() unexpected error: %v", err)
	}

	apiKey := req.Header.Get("DD-API-KEY")
	if apiKey != "secret-api-key" {
		t.Errorf("DD-API-KEY header = %q, want secret-api-key", apiKey)
	}
}

func TestAlertTypeFor(t *testing.T) {
	tests := []struct {
		state    domain.State
		expected string
	}{
		{domain.StateSuccess, alertSuccess},
		{domain.StateFailure, alertError},
		{domain.StateError, alertError},
		{domain.StateRunning, alertInfo},
		{domain.StatePending, alertInfo},
		{domain.StateCanceled, alertInfo},
		{domain.State("unknown"), alertInfo},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			result := alertTypeFor(tt.state)
			if result != tt.expected {
				t.Errorf("alertTypeFor(%s) = %s, want %s", tt.state, result, tt.expected)
			}
		})
	}
}

func TestURL(t *testing.T) {
	tests := []struct {
		name     string
		site     string
		expected string
	}{
		{
			name:     "default site",
			site:     "datadoghq.com",
			expected: "https://api.datadoghq.com/api/v2/events",
		},
		{
			name:     "EU site",
			site:     "testSiteEU",
			expected: "https://api.testSiteEU/api/v2/events",
		},
		{
			name:     "custom site",
			site:     "custom.datadog.example.com",
			expected: "https://api.custom.datadog.example.com/api/v2/events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := New(Config{
				APIKey: scm.NewStaticToken(testAPIKeyValue),
				Site:   tt.site,
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

func TestSanitizeTag(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with/slash", "with_slash"},
		{"with:colon", "with_colon"},
		{"multiple/slashes/here", "multiple_slashes_here"},
		{"ci/cd:build", "ci_cd_build"},
		{"no-special-chars", "no-special-chars"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeTag(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeTag(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
