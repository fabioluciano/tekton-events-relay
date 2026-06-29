package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	testURL       = "https://test"
	testRunID     = "run-123"
	testBuild     = "test/build"
	testNamespace = "default"
	testChannelID = "channel-id-123"
)

type staticRefresher struct{ tok string }

func (s staticRefresher) Token(_ context.Context) (string, error) { return s.tok, nil }

type rotatingRefresher struct{ n int }

func (r *rotatingRefresher) Token(_ context.Context) (string, error) {
	r.n++
	return fmt.Sprintf("token-%d", r.n), nil
}

func TestNew(t *testing.T) {
	cfg := Config{WebhookURL: "https://mattermost.example.com/hooks/test"}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if n == nil {
		t.Fatal("expected notifier, got nil")
	}
	if n.cfg.Username != "tekton-events-relay" {
		t.Errorf("username = %q, want tekton-events-relay", n.cfg.Username)
	}
}

func TestNew_CustomConfig(t *testing.T) {
	cfg := Config{
		WebhookURL: "https://mattermost.example.com/hooks/test",
		Channel:    "#custom-channel",
		Username:   "custom-bot",
		IconURL:    "https://example.com/icon.png",
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if n.cfg.Channel != "#custom-channel" {
		t.Errorf("channel = %q, want #custom-channel", n.cfg.Channel)
	}
	if n.cfg.Username != "custom-bot" {
		t.Errorf("username = %q, want custom-bot", n.cfg.Username)
	}
	if n.cfg.IconURL != "https://example.com/icon.png" {
		t.Errorf("icon_url = %q, want https://example.com/icon.png", n.cfg.IconURL)
	}
}

func TestName(t *testing.T) {
	n, err := New(Config{WebhookURL: testURL}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if n.Name() != "mattermost" {
		t.Errorf("Name() = %q, want mattermost", n.Name())
	}
}

func TestType(t *testing.T) {
	n, err := New(Config{WebhookURL: testURL}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if n.Type() != notifier.ActionNotify {
		t.Errorf("Type() = %q, want %q", n.Type(), notifier.ActionNotify)
	}
}

func TestClose(t *testing.T) {
	n, err := New(Config{WebhookURL: testURL}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if err := n.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestHandle_WebhookMode(t *testing.T) {
	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{WebhookURL: server.URL}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{
		RunID:     testRunID,
		RunName:   testRunID,
		State:     domain.StateSuccess,
		Context:   testBuild,
		Namespace: testNamespace,
	}

	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	if receivedPayload == nil {
		t.Fatal("expected payload to be sent")
	}
	if receivedPayload["username"] != "tekton-events-relay" {
		t.Errorf("username = %v, want tekton-events-relay", receivedPayload["username"])
	}
	if _, ok := receivedPayload["text"]; !ok {
		t.Error("expected 'text' field in payload")
	}
}

func TestHandle_WebhookMode_WithChannel(t *testing.T) {
	receivedPayload := runWebhookTest(t, Config{Channel: "#alerts"}, nil)
	if receivedPayload["channel"] != "#alerts" {
		t.Errorf("channel = %v, want #alerts", receivedPayload["channel"])
	}
}

func TestHandle_WebhookMode_WithIconURL(t *testing.T) {
	receivedPayload := runWebhookTest(t, Config{IconURL: "https://example.com/icon.png"}, nil)
	if receivedPayload["icon_url"] != "https://example.com/icon.png" {
		t.Errorf("icon_url = %v, want https://example.com/icon.png", receivedPayload["icon_url"])
	}
}

func runWebhookTest(t *testing.T, cfg Config, evt *domain.Event) map[string]any {
	t.Helper()
	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg.WebhookURL = server.URL
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if evt == nil {
		evt = &domain.Event{
			RunID:     testRunID,
			RunName:   testRunID,
			State:     domain.StateSuccess,
			Context:   testBuild,
			Namespace: testNamespace,
		}
	}

	if err := n.Handle(context.Background(), *evt); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}
	return receivedPayload
}

func TestHandle_BotTokenMode(t *testing.T) {
	var receivedPayload map[string]any
	var authHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cfg := Config{
		Token:     staticRefresher{tok: "test-token"},
		BaseURL:   server.URL,
		ChannelID: testChannelID,
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{
		RunID:     testRunID,
		RunName:   testRunID,
		State:     domain.StateSuccess,
		Context:   testBuild,
		Namespace: testNamespace,
	}

	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	if authHeader != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", authHeader, "Bearer test-token")
	}
	if receivedPayload["channel_id"] != testChannelID {
		t.Errorf("channel_id = %v, want %v", receivedPayload["channel_id"], testChannelID)
	}
}

func TestHandle_BotTokenMode_TokenRefreshedPerRequest(t *testing.T) {
	var authHeaders []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cfg := Config{
		Token:     &rotatingRefresher{},
		BaseURL:   server.URL,
		ChannelID: testChannelID,
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{
		RunID:     testRunID,
		RunName:   testRunID,
		State:     domain.StateSuccess,
		Context:   testBuild,
		Namespace: testNamespace,
	}

	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("first Handle() failed: %v", err)
	}
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("second Handle() failed: %v", err)
	}

	if len(authHeaders) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(authHeaders))
	}
	if authHeaders[0] == authHeaders[1] {
		t.Errorf("token not refreshed per request: both calls used %q", authHeaders[0])
	}
	if authHeaders[0] != "Bearer token-1" || authHeaders[1] != "Bearer token-2" {
		t.Errorf("auths = %v, want rotating Bearer tokens", authHeaders)
	}
}

func TestHandle_BotTokenMode_WithChannelOverride(t *testing.T) {
	var receivedPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cfg := Config{
		Token:     staticRefresher{tok: "test-token"},
		BaseURL:   server.URL,
		ChannelID: testChannelID,
		Channel:   "overridden-channel",
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{
		RunID:     testRunID,
		RunName:   testRunID,
		State:     domain.StateSuccess,
		Context:   testBuild,
		Namespace: testNamespace,
	}

	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	if receivedPayload["channel_id"] != "overridden-channel" {
		t.Errorf("channel_id = %v, want overridden-channel", receivedPayload["channel_id"])
	}
}

func TestHandle_BotTokenMode_BaseURLTrailingSlash(t *testing.T) {
	var requestPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cfg := Config{
		Token:     staticRefresher{tok: "test-token"},
		BaseURL:   server.URL + "/",
		ChannelID: testChannelID,
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{
		RunID:     testRunID,
		RunName:   testRunID,
		State:     domain.StateSuccess,
		Context:   testBuild,
		Namespace: testNamespace,
	}

	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	if requestPath != "/api/v4/posts" {
		t.Errorf("request path = %q, want /api/v4/posts", requestPath)
	}
}

func TestHandle_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := Config{WebhookURL: server.URL}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{
		RunID:     testRunID,
		RunName:   testRunID,
		State:     domain.StateSuccess,
		Context:   testBuild,
		Namespace: testNamespace,
	}

	err = n.Handle(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
}

func TestTemplateFile_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := tmpDir + "/template.txt"
	if err := writeFile(templatePath, "Build {{.State}} for {{.RunName}}"); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	cfg := Config{
		WebhookURL: testURL,
		Template:   templatePath,
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() with valid template file failed: %v", err)
	}
	if n.tmpl == nil {
		t.Fatal("expected template to be loaded")
	}
}

func TestTemplateFile_Missing(t *testing.T) {
	cfg := Config{
		WebhookURL: testURL,
		Template:   "/nonexistent/template.txt",
	}
	_, err := New(cfg, nil)
	if err == nil {
		t.Fatal("expected error for missing template file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read template file") {
		t.Errorf("error = %v, want 'failed to read template file'", err)
	}
}

func TestTemplateFile_InvalidSyntax(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := tmpDir + "/invalid.txt"
	if err := writeFile(templatePath, "{{.Invalid"); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	cfg := Config{
		WebhookURL: testURL,
		Template:   templatePath,
	}
	_, err := New(cfg, nil)
	if err == nil {
		t.Fatal("expected error for invalid template syntax, got nil")
	}
	if !strings.Contains(err.Error(), "invalid template") {
		t.Errorf("error = %v, want 'invalid template'", err)
	}
}

func TestHandle_MultipleStates(t *testing.T) {
	states := []domain.State{
		domain.StateSuccess,
		domain.StateFailure,
		domain.StateError,
		domain.StateRunning,
		domain.StatePending,
		domain.StateCanceled,
	}

	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				callCount++
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			cfg := Config{WebhookURL: server.URL}
			n, err := New(cfg, nil)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}

			evt := domain.Event{
				RunID:       testRunID,
				RunName:     testRunID,
				State:       state,
				Context:     testBuild,
				Namespace:   testNamespace,
				Description: "test description",
			}

			if err := n.Handle(context.Background(), evt); err != nil {
				t.Fatalf("Handle() error: %v", err)
			}

			if callCount != 1 {
				t.Errorf("expected 1 webhook call, got %d", callCount)
			}
		})
	}
}

func TestDefaultText(t *testing.T) {
	tests := []struct {
		name string
		evt  domain.Event
		want string
	}{
		{
			name: "success",
			evt:  domain.Event{State: domain.StateSuccess, Context: "ci/build", Description: "passed"},
			want: ":white_check_mark: **ci/build** — passed",
		},
		{
			name: "failure",
			evt:  domain.Event{State: domain.StateFailure, Context: "ci/build", Description: "failed"},
			want: ":x: **ci/build** — failed",
		},
		{
			name: "running",
			evt:  domain.Event{State: domain.StateRunning, Context: "ci/build", Description: "in progress"},
			want: ":hourglass_flowing_sand: **ci/build** — in progress",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultText(tt.evt)
			if got != tt.want {
				t.Errorf("defaultText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultText_WithTargetURL(t *testing.T) {
	evt := domain.Event{
		State:       domain.StateSuccess,
		Context:     "ci/build",
		Description: "passed",
		TargetURL:   "https://dashboard.example.com/run/123",
	}

	got := defaultText(evt)
	if !strings.Contains(got, "[View run](https://dashboard.example.com/run/123)") {
		t.Errorf("expected View run link, got: %s", got)
	}
}

func TestEmojiFor(t *testing.T) {
	tests := []struct {
		state domain.State
		want  string
	}{
		{domain.StateSuccess, ":white_check_mark:"},
		{domain.StateFailure, emojiFailure},
		{domain.StateError, emojiFailure},
		{domain.StateRunning, ":hourglass_flowing_sand:"},
		{domain.StatePending, emojiUnknown},
		{domain.StateCanceled, emojiUnknown},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := emojiFor(tt.state)
			if got != tt.want {
				t.Errorf("emojiFor(%v) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestHandle_WithCustomTemplate(t *testing.T) {
	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		WebhookURL: server.URL,
		Template:   "Pipeline {{.RunName}} is {{.State}}",
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{
		RunID:     testRunID,
		RunName:   testRunID,
		State:     domain.StateSuccess,
		Context:   testBuild,
		Namespace: testNamespace,
	}

	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	text, ok := receivedPayload["text"].(string)
	if !ok {
		t.Fatal("expected text field in payload")
	}
	if !strings.Contains(text, "Pipeline run-123 is success") {
		t.Errorf("text = %q, expected template output", text)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0600) //nolint:gosec // test helper
}
