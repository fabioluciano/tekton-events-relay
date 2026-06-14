package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	testWebhookURL   = "https://test"
	testPipeline     = "test-pipeline"
	testBuildSuccess = "Build succeeded"
	testNamespace    = "default"
	testContext      = "test-context"
	testContextShort = "test"
	testRunID        = "run-1"
)

func TestNew(t *testing.T) {
	t.Run("with minimal config", func(t *testing.T) {
		n, err := New(Config{WebhookURL: testWebhookURL}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		if n == nil {
			t.Fatal("expected notifier")
		}
		if n.cfg.Username != defaultUsername {
			t.Errorf("Username = %q, want tekton-events-relay", n.cfg.Username)
		}
	})

	t.Run("with custom username", func(t *testing.T) {
		n, err := New(Config{
			WebhookURL: testWebhookURL,
			Username:   "custom-bot",
		}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		if n.cfg.Username != "custom-bot" {
			t.Errorf("Username = %q, want custom-bot", n.cfg.Username)
		}
	})
}

func TestNew_BotTokenMode(t *testing.T) {
	var receivedAuth string
	var receivedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedURL = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{ //nolint:gosec // test-only credential values
		BotToken:  "bot-test-token",
		ChannelID: "123456789",
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Patch BuildURL to point at test server
	n.base.BuildURL = func(_ domain.Event) (string, error) {
		return server.URL + "/api/v10/channels/123456789/messages", nil
	}

	evt := domain.Event{
		RunID:     testRunID,
		RunName:   testRunID,
		State:     domain.StateSuccess,
		Context:   testContext,
		Namespace: testNamespace,
	}

	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	if receivedAuth != "Bot bot-test-token" {
		t.Errorf("Authorization = %q, want %q", receivedAuth, "Bot bot-test-token")
	}
	if !strings.Contains(receivedURL, "messages") {
		t.Errorf("URL = %q, expected messages endpoint", receivedURL)
	}
}

func TestName(t *testing.T) {
	n, err := New(Config{WebhookURL: "https://test"}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if n.Name() != "discord" {
		t.Errorf("Name() = %q, want discord", n.Name())
	}
}

func TestType(t *testing.T) {
	n, err := New(Config{WebhookURL: "https://test"}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if n.Type() != notifier.ActionNotify {
		t.Errorf("Type() = %q, want %q", n.Type(), notifier.ActionNotify)
	}
}

func TestHandle(t *testing.T) {
	t.Run("success case", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("Method = %q, want POST", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
			}
			if r.Header.Get("User-Agent") != "tekton-events-relay" {
				t.Errorf("User-Agent = %q, want tekton-events-relay", r.Header.Get("User-Agent"))
			}

			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("failed to unmarshal payload: %v", err)
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		n, err := New(Config{WebhookURL: server.URL}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}

		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     testPipeline,
			Description: testBuildSuccess,
			RunID:       "run-123",
			RunName:     "run-123",
			Namespace:   testNamespace,
		}

		err = n.Handle(context.Background(), event)
		if err != nil {
			t.Errorf("Handle() error = %v, want nil", err)
		}
	})

	t.Run("returns error on HTTP failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		}))
		defer server.Close()

		n, err := New(Config{WebhookURL: server.URL}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}

		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     testPipeline,
			Description: testBuildSuccess,
		}

		err = n.Handle(context.Background(), event)
		if err == nil {
			t.Error("Handle() error = nil, want error")
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		n, err := New(Config{WebhookURL: server.URL}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     testPipeline,
			Description: testBuildSuccess,
		}

		err = n.Handle(ctx, event)
		if err == nil {
			t.Error("Handle() with cancelled context should return error")
		}
	})
}

//nolint:gocyclo // test covers all payload field combinations
func TestPayload(t *testing.T) {
	t.Run("minimal event", func(t *testing.T) {
		n, err := New(Config{WebhookURL: testWebhookURL}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     testContext,
			Description: "test description",
			RunID:       "run-123",
			RunName:     "run-123",
			Namespace:   testNamespace,
		}

		payload, err := n.payload(event)
		if err != nil {
			t.Fatalf("payload() error = %v, want nil", err)
		}

		p, ok := payload.(map[string]any)
		if !ok {
			t.Fatal("payload is not map[string]any")
		}

		if p["username"] != "tekton-events-relay" {
			t.Errorf("username = %v, want tekton-events-relay", p["username"])
		}

		embeds, ok := p["embeds"].([]any)
		if !ok || len(embeds) != 1 {
			t.Fatal("embeds is not []any with 1 element")
		}

		embed := embeds[0].(map[string]any)
		if embed["title"] != "test-context" {
			t.Errorf("embed title = %v, want test-context", embed["title"])
		}
		if embed["description"] != "test description" {
			t.Errorf("embed description = %v, want test description", embed["description"])
		}
		if embed["color"] != 0x36a64f {
			t.Errorf("embed color = %v, want %v", embed["color"], 0x36a64f)
		}
	})

	t.Run("event with commit SHA", func(t *testing.T) {
		n, err := New(Config{WebhookURL: testWebhookURL}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		event := domain.Event{
			State:       domain.StateFailure,
			Context:     testContext,
			Description: "test failed",
			RunID:       "run-456",
			RunName:     "run-456",
			Namespace:   testNamespace,
			CommitSHA:   "1234567890abcdef",
		}

		payload, err := n.payload(event)
		if err != nil {
			t.Fatalf("payload() error = %v, want nil", err)
		}

		p := payload.(map[string]any)
		embeds := p["embeds"].([]any)
		embed := embeds[0].(map[string]any)
		fields := embed["fields"].([]map[string]any)

		found := false
		for _, field := range fields {
			if field["name"] == fieldCommit {
				found = true
				if field["value"] != "`1234567`" {
					t.Errorf("commit field value = %v, want `1234567`", field["value"])
				}
			}
		}
		if !found {
			t.Error("commit field not found in payload")
		}
	})

	t.Run("event with short commit SHA", func(t *testing.T) {
		n, err := New(Config{WebhookURL: testWebhookURL}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     testContextShort,
			Description: "ok",
			RunID:       testRunID,
			RunName:     testRunID,
			Namespace:   testNamespace,
			CommitSHA:   "abc123",
		}

		payload, err := n.payload(event)
		if err != nil {
			t.Fatalf("payload() error = %v, want nil", err)
		}

		p := payload.(map[string]any)
		embeds := p["embeds"].([]any)
		embed := embeds[0].(map[string]any)
		fields := embed["fields"].([]map[string]any)

		for _, field := range fields {
			if field["name"] == fieldCommit {
				if field["value"] != "`abc123`" {
					t.Errorf("commit field value = %v, want `abc123`", field["value"])
				}
			}
		}
	})

	t.Run("event with target URL", func(t *testing.T) {
		n, err := New(Config{WebhookURL: testWebhookURL}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		event := domain.Event{
			State:       domain.StateRunning,
			Context:     "build",
			Description: "building",
			RunID:       "run-789",
			RunName:     "run-789",
			Namespace:   testNamespace,
			TargetURL:   "https://dashboard.example.com/runs/789",
		}

		payload, err := n.payload(event)
		if err != nil {
			t.Fatalf("payload() error = %v, want nil", err)
		}

		p := payload.(map[string]any)
		embeds := p["embeds"].([]any)
		embed := embeds[0].(map[string]any)

		if embed["url"] != "https://dashboard.example.com/runs/789" {
			t.Errorf("embed url = %v, want https://dashboard.example.com/runs/789", embed["url"])
		}
	})

	t.Run("event without target URL", func(t *testing.T) {
		n, err := New(Config{WebhookURL: testWebhookURL}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     testContextShort,
			Description: "ok",
			RunID:       testRunID,
			RunName:     testRunID,
			Namespace:   testNamespace,
		}

		payload, err := n.payload(event)
		if err != nil {
			t.Fatalf("payload() error = %v, want nil", err)
		}

		p := payload.(map[string]any)
		embeds := p["embeds"].([]any)
		embed := embeds[0].(map[string]any)

		if _, exists := embed["url"]; exists {
			t.Error("embed url should not be present")
		}
	})

	t.Run("footer contains namespace and run ID", func(t *testing.T) {
		n, err := New(Config{WebhookURL: testWebhookURL}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     testContextShort,
			Description: "ok",
			RunID:       "my-run",
			RunName:     "my-run",
			Namespace:   "production",
		}

		payload, err := n.payload(event)
		if err != nil {
			t.Fatalf("payload() error = %v, want nil", err)
		}

		p := payload.(map[string]any)
		embeds := p["embeds"].([]any)
		embed := embeds[0].(map[string]any)
		footer := embed["footer"].(map[string]string)

		expected := "production/my-run"
		if footer["text"] != expected {
			t.Errorf("footer text = %q, want %q", footer["text"], expected)
		}
	})

	t.Run("custom username in payload", func(t *testing.T) {
		n, err := New(Config{
			WebhookURL: testWebhookURL,
			Username:   "my-custom-bot",
		}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		event := domain.Event{
			State:       domain.StateSuccess,
			Context:     testContextShort,
			Description: "ok",
			RunID:       testRunID,
			RunName:     testRunID,
			Namespace:   testNamespace,
		}

		payload, err := n.payload(event)
		if err != nil {
			t.Fatalf("payload() error = %v, want nil", err)
		}

		p := payload.(map[string]any)
		if p["username"] != "my-custom-bot" {
			t.Errorf("username = %v, want my-custom-bot", p["username"])
		}
	})
}

func TestColorFor(t *testing.T) {
	tests := []struct {
		name  string
		state domain.State
		want  int
	}{
		{
			name:  "success is green",
			state: domain.StateSuccess,
			want:  0x36a64f,
		},
		{
			name:  "failure is red",
			state: domain.StateFailure,
			want:  0xe01e5a,
		},
		{
			name:  "error is red",
			state: domain.StateError,
			want:  0xe01e5a,
		},
		{
			name:  "running is yellow",
			state: domain.StateRunning,
			want:  0xdaa038,
		},
		{
			name:  "pending is gray",
			state: domain.StatePending,
			want:  0xaaaaaa,
		},
		{
			name:  "canceled is gray",
			state: domain.StateCanceled,
			want:  0xaaaaaa,
		},
		{
			name:  "unknown state is gray",
			state: domain.State("unknown"),
			want:  0xaaaaaa,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := colorFor(tt.state)
			if got != tt.want {
				t.Errorf("colorFor(%v) = 0x%x, want 0x%x", tt.state, got, tt.want)
			}
		})
	}
}

func TestTemplateFile_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "template.txt")
	if err := os.WriteFile(templatePath, []byte("Build {{.State}} for {{.RunName}}"), 0600); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	cfg := Config{
		WebhookURL: testWebhookURL,
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
		WebhookURL: testWebhookURL,
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
	templatePath := filepath.Join(tmpDir, "invalid.txt")
	if err := os.WriteFile(templatePath, []byte("{{.Invalid"), 0600); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	cfg := Config{
		WebhookURL: testWebhookURL,
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
