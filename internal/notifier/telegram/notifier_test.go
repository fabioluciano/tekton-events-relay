package telegram

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
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

func TestEscapeMarkdownV2(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "underscore",
			input: "my_pipeline",
			want:  "my\\_pipeline",
		},
		{
			name:  "dash and underscore",
			input: "my-pipeline_run",
			want:  "my\\-pipeline\\_run",
		},
		{
			name:  "all special chars",
			input: "_*[]()~`>#+-=|{}.!",
			want:  "\\_\\*\\[\\]\\(\\)\\~\\`\\>\\#\\+\\-\\=\\|\\{\\}\\.\\!",
		},
		{
			name:  "RunName with special chars",
			input: "Pipeline my-app_task-1 - success",
			want:  "Pipeline my\\-app\\_task\\-1 \\- success",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeMarkdownV2(tt.input)
			if got != tt.want {
				t.Errorf("EscapeMarkdownV2(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

type recordingToken struct {
	val string
}

func (r *recordingToken) Token(_ context.Context) (string, error) {
	return r.val, nil
}

func TestNew_RequiresToken(t *testing.T) {
	_, err := New(Config{
		Token:  nil,
		ChatID: "123",
	}, zap.NewNop())
	if err == nil {
		t.Fatal("expected error when token is nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("expected error about token, got: %s", err.Error())
	}
}

func TestNew_RequiresChatID(t *testing.T) {
	_, err := New(Config{
		Token:  &recordingToken{val: "test-token"},
		ChatID: "",
	}, zap.NewNop())
	if err == nil {
		t.Fatal("expected error when chat_id is empty")
	}
	if !strings.Contains(err.Error(), "chat_id") {
		t.Errorf("expected error about chat_id, got: %s", err.Error())
	}
}

func TestHandle_SendsMarkdownV2Payload(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/bot") {
			t.Errorf("expected URL path to contain /bot, got %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.Path, "/sendMessage") {
			t.Errorf("expected URL path to contain /sendMessage, got %s", r.URL.Path)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %s", err)
		}
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Fatalf("unmarshal body: %s", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	n := &Notifier{
		name:   "test-telegram",
		chatID: "-1001234567890",
	}

	// Use default template
	tmpl, err := scm.CompileTemplate("telegram", defaultTemplateContent, nil)
	if err != nil {
		t.Fatalf("compile template: %s", err)
	}
	n.tmpl = tmpl

	// Build the base with the test server URL — we override BuildURL to use
	// the test server instead of the real Telegram API.
	n.base = buildTestBase(srv.URL, n, zap.NewNop())

	e := domain.Event{
		RunName: "my-pipeline_task-1",
		State:   domain.StateSuccess,
	}

	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle() error: %s", err)
	}

	if receivedBody == nil {
		t.Fatal("expected request body to be recorded")
	}

	chatID, ok := receivedBody["chat_id"].(string)
	if !ok || chatID != "-1001234567890" {
		t.Errorf("expected chat_id '-1001234567890', got %v", receivedBody["chat_id"])
	}

	parseMode, ok := receivedBody["parse_mode"].(string)
	if !ok || parseMode != "MarkdownV2" {
		t.Errorf("expected parse_mode 'MarkdownV2', got %v", receivedBody["parse_mode"])
	}

	text, ok := receivedBody["text"].(string)
	if !ok {
		t.Fatalf("expected text to be a string, got %T", receivedBody["text"])
	}

	// The rendered text should have MarkdownV2 escapes for _ and -
	if !strings.Contains(text, "\\_") {
		t.Errorf("expected escaped underscore in text, got: %s", text)
	}
	if !strings.Contains(text, "\\-") {
		t.Errorf("expected escaped dash in text, got: %s", text)
	}
}

func TestHandle_TokenInURL(t *testing.T) {
	var requestPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	n := &Notifier{
		name:   "test-telegram",
		chatID: "12345",
	}
	tmpl, err := scm.CompileTemplate("telegram", defaultTemplateContent, nil)
	if err != nil {
		t.Fatalf("compile template: %s", err)
	}
	n.tmpl = tmpl
	n.base = buildTestBaseWithToken(srv.URL, "my-secret-bot-token", n, zap.NewNop())

	e := domain.Event{
		RunName: "test-run",
		State:   domain.StateRunning,
	}

	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle() error: %s", err)
	}

	if !strings.Contains(requestPath, "my-secret-bot-token") {
		t.Errorf("expected token in URL path, got: %s", requestPath)
	}
	if !strings.Contains(requestPath, "/sendMessage") {
		t.Errorf("expected /sendMessage in path, got: %s", requestPath)
	}
}

func TestName(t *testing.T) {
	n := &Notifier{name: "my-telegram"}
	if n.Name() != "my-telegram" {
		t.Errorf("Name() = %q, want %q", n.Name(), "my-telegram")
	}
}

func TestType(t *testing.T) {
	n := &Notifier{}
	if string(n.Type()) != "notify" {
		t.Errorf("Type() = %q, want %q", string(n.Type()), "notify")
	}
}

func TestClose(t *testing.T) {
	n := &Notifier{}
	if err := n.Close(); err != nil {
		t.Errorf("Close() returned error: %s", err)
	}
}

func buildTestBase(serverURL string, n *Notifier, log *zap.Logger) *notifier.Base {
	return &notifier.Base{
		HTTP:         http.DefaultClient,
		UserAgent:    "test",
		Log:          log,
		BuildURL:     func(_ domain.Event) (string, error) { return serverURL + "/bot-test-token/sendMessage", nil },
		BuildPayload: n.payload,
	}
}

func buildTestBaseWithToken(serverURL, token string, n *Notifier, log *zap.Logger) *notifier.Base {
	return &notifier.Base{
		HTTP:         http.DefaultClient,
		UserAgent:    "test",
		Log:          log,
		BuildURL:     func(_ domain.Event) (string, error) { return serverURL + "/bot" + token + "/sendMessage", nil },
		BuildPayload: n.payload,
	}
}
