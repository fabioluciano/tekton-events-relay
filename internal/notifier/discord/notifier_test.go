package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/msgstore"
)

const (
	testPipeline     = "test-pipeline"
	testBuildSuccess = "Build succeeded"
	testNamespace    = "default"
	testContext      = "test-context"
	testRunID        = "run-1"
	testChannelID    = "123456789"
)

// webhookURL returns a Discord-form webhook URL pointing at host. The notifier
// pins requests to this host, so the SDK's calls land on the stub server.
func webhookURL(host string) string { return host + "/api/webhooks/123/abc" }

// recordingServer captures the method, path and Authorization of every request
// and replies with a minimal message body so WebhookExecute(wait=true) can
// unmarshal a message ID.
type recordingServer struct {
	mu      sync.Mutex
	methods []string
	paths   []string
	auths   []string
}

func newRecordingServer(t *testing.T) (*httptest.Server, *recordingServer) {
	t.Helper()
	rec := &recordingServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.methods = append(rec.methods, r.Method)
		rec.paths = append(rec.paths, r.URL.Path)
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg-1"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// redirectClient returns an http.Client whose transport pins every request at
// host, used to steer bot-mode SDK calls (which target discord.com) at a stub.
func redirectClient(host string) *http.Client {
	u, _ := url.Parse(host)
	return &http.Client{Transport: &hostRewriteTransport{base: http.DefaultTransport, scheme: u.Scheme, host: u.Host}}
}

type staticRefresher struct{ tok string }

func (s staticRefresher) Token(_ context.Context) (string, error) { return s.tok, nil }

type rotatingRefresher struct{ n int }

func (r *rotatingRefresher) Token(_ context.Context) (string, error) {
	r.n++
	return fmt.Sprintf("bot-token-%d", r.n), nil
}

func successEvent() domain.Event {
	return domain.Event{
		RunID: testRunID, RunName: testRunID, State: domain.StateSuccess,
		Context: testContext, Description: testBuildSuccess, Namespace: testNamespace,
	}
}

func TestNew(t *testing.T) {
	srv, _ := newRecordingServer(t)

	t.Run("webhook minimal config parses id/token and defaults username", func(t *testing.T) {
		n, err := New(Config{WebhookURL: webhookURL(srv.URL)}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		if n.cfg.Username != defaultUsername {
			t.Errorf("Username = %q, want %q", n.cfg.Username, defaultUsername)
		}
		if n.webhookID != "123" || n.webhookToken != "abc" {
			t.Errorf("parsed id/token = %q/%q, want 123/abc", n.webhookID, n.webhookToken)
		}
		if n.botMode {
			t.Error("expected webhook mode, got bot mode")
		}
	})

	t.Run("custom username", func(t *testing.T) {
		n, err := New(Config{WebhookURL: webhookURL(srv.URL), Username: "custom-bot"}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		if n.cfg.Username != "custom-bot" {
			t.Errorf("Username = %q, want custom-bot", n.cfg.Username)
		}
	})

	t.Run("invalid webhook URL errors", func(t *testing.T) {
		if _, err := New(Config{WebhookURL: "https://example.com/nope"}, nil); err == nil {
			t.Fatal("expected error for malformed webhook URL")
		}
	})
}

func TestNew_BotTokenMode(t *testing.T) {
	srv, rec := newRecordingServer(t)

	cfg := Config{
		BotToken:   staticRefresher{tok: "bot-test-token"},
		ChannelID:  testChannelID,
		httpClient: redirectClient(srv.URL),
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if !n.botMode {
		t.Fatal("expected bot mode")
	}

	if err := n.Handle(context.Background(), successEvent()); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	if len(rec.paths) != 1 || !strings.Contains(rec.paths[0], "channels/"+testChannelID+"/messages") {
		t.Errorf("paths = %v, expected one channel messages call", rec.paths)
	}
	if rec.methods[0] != http.MethodPost {
		t.Errorf("method = %q, want POST", rec.methods[0])
	}
	if rec.auths[0] != "Bot bot-test-token" {
		t.Errorf("Authorization = %q, want %q", rec.auths[0], "Bot bot-test-token")
	}
}

func TestName(t *testing.T) {
	srv, _ := newRecordingServer(t)
	n, err := New(Config{WebhookURL: webhookURL(srv.URL)}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if n.Name() != "discord" {
		t.Errorf("Name() = %q, want discord", n.Name())
	}
}

func TestType(t *testing.T) {
	srv, _ := newRecordingServer(t)
	n, err := New(Config{WebhookURL: webhookURL(srv.URL)}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if n.Type() != notifier.ActionNotify {
		t.Errorf("Type() = %q, want %q", n.Type(), notifier.ActionNotify)
	}
}

func TestHandle(t *testing.T) {
	t.Run("webhook success", func(t *testing.T) {
		srv, rec := newRecordingServer(t)
		n, err := New(Config{WebhookURL: webhookURL(srv.URL)}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		if err := n.Handle(context.Background(), successEvent()); err != nil {
			t.Errorf("Handle() error = %v, want nil", err)
		}
		if len(rec.paths) != 1 || rec.methods[0] != http.MethodPost {
			t.Errorf("expected one POST webhook execute, got methods=%v paths=%v", rec.methods, rec.paths)
		}
		if !strings.HasSuffix(rec.paths[0], "/webhooks/123/abc") {
			t.Errorf("path = %q, want webhook execute endpoint", rec.paths[0])
		}
	})

	t.Run("returns error on HTTP failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
		}))
		defer srv.Close()

		n, err := New(Config{WebhookURL: webhookURL(srv.URL)}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		if err := n.Handle(context.Background(), successEvent()); err == nil {
			t.Error("Handle() error = nil, want error")
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		srv, _ := newRecordingServer(t)
		n, err := New(Config{WebhookURL: webhookURL(srv.URL)}, nil)
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := n.Handle(ctx, successEvent()); err == nil {
			t.Error("Handle() with cancelled context should return error")
		}
	})
}

func TestWebhook_Create_CapturesID(t *testing.T) {
	srv, rec := newRecordingServer(t)
	store := msgstore.NewMemoryStore(0, 0)
	n, err := New(Config{WebhookURL: webhookURL(srv.URL), Mode: ModeUpsert, MessageStore: store}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := n.Handle(context.Background(), successEvent()); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	// Given a create that returns a message ID, When wait=true, Then the ID is stored.
	if id, ok := store.Load(testRunID); !ok || id != "msg-1" {
		t.Errorf("stored id = %q, ok=%v; want msg-1", id, ok)
	}
	if rec.methods[0] != http.MethodPost {
		t.Errorf("first call method = %q, want POST", rec.methods[0])
	}
}

func TestWebhook_Upsert_SecondCallEdits(t *testing.T) {
	srv, rec := newRecordingServer(t)
	n, err := New(Config{WebhookURL: webhookURL(srv.URL), Mode: ModeUpsert, MessageStore: msgstore.NewMemoryStore(0, 0)}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := successEvent()
	evt.State = domain.StateRunning
	// First call posts and stores the message ID.
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("first Handle() failed: %v", err)
	}
	// Second call for the same RunID must edit via WebhookMessageEdit.
	evt.State = domain.StateSuccess
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("second Handle() failed: %v", err)
	}

	if len(rec.methods) != 2 {
		t.Fatalf("expected 2 calls, got %d (%v)", len(rec.methods), rec.paths)
	}
	if rec.methods[0] != http.MethodPost {
		t.Errorf("first method = %q, want POST (execute)", rec.methods[0])
	}
	if rec.methods[1] != http.MethodPatch || !strings.Contains(rec.paths[1], "/messages/msg-1") {
		t.Errorf("second call = %s %s, want PATCH .../messages/msg-1", rec.methods[1], rec.paths[1])
	}
}

func TestWebhook_Upsert_FailsOpenWithoutStore(t *testing.T) {
	srv, rec := newRecordingServer(t)
	// No MessageStore: upsert must degrade to a plain execute each time.
	n, err := New(Config{WebhookURL: webhookURL(srv.URL), Mode: ModeUpsert}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := successEvent()
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("first Handle() failed: %v", err)
	}
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("second Handle() failed: %v", err)
	}

	if len(rec.methods) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(rec.methods))
	}
	for i, m := range rec.methods {
		if m != http.MethodPost {
			t.Errorf("call %d method = %q, want POST (fail-open to execute)", i, m)
		}
	}
}

func TestBotToken_TokenRefreshedPerRequest(t *testing.T) {
	srv, rec := newRecordingServer(t)
	n, err := New(Config{
		BotToken:   &rotatingRefresher{},
		ChannelID:  testChannelID,
		httpClient: redirectClient(srv.URL),
	}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := successEvent()
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("first Handle() failed: %v", err)
	}
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("second Handle() failed: %v", err)
	}

	if len(rec.auths) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(rec.auths))
	}
	if rec.auths[0] == rec.auths[1] {
		t.Errorf("token not refreshed per request: both calls used %q", rec.auths[0])
	}
	if rec.auths[0] != "Bot bot-token-1" || rec.auths[1] != "Bot bot-token-2" {
		t.Errorf("auths = %v, want rotating Bot tokens", rec.auths)
	}
}

func TestEmbed(t *testing.T) {
	srv, _ := newRecordingServer(t)
	n, err := New(Config{WebhookURL: webhookURL(srv.URL)}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	t.Run("default embed fields", func(t *testing.T) {
		e := domain.Event{State: domain.StateSuccess, Context: testContext, Description: "d", RunName: "r", Namespace: "ns", CommitSHA: "1234567890abcdef", TargetURL: "https://x/y"}
		em := n.embed(e)
		if em.Title != testContext || em.Description != "d" {
			t.Errorf("title/desc = %q/%q", em.Title, em.Description)
		}
		if em.Color != 0x36a64f {
			t.Errorf("color = %#x, want 0x36a64f", em.Color)
		}
		if em.URL != "https://x/y" {
			t.Errorf("url = %q", em.URL)
		}
		if em.Footer == nil || em.Footer.Text != "ns/r" {
			t.Errorf("footer = %+v, want ns/r", em.Footer)
		}
		var commit string
		for _, f := range em.Fields {
			if f.Name == fieldCommit {
				commit = f.Value
			}
		}
		if commit != "`1234567`" {
			t.Errorf("commit field = %q, want `1234567`", commit)
		}
	})

	t.Run("no commit field when SHA empty", func(t *testing.T) {
		em := n.embed(domain.Event{State: domain.StateRunning, RunName: "r", Namespace: "ns"})
		for _, f := range em.Fields {
			if f.Name == fieldCommit {
				t.Error("commit field should be absent")
			}
		}
	})
}

func TestColorFor(t *testing.T) {
	tests := []struct {
		name  string
		state domain.State
		want  int
	}{
		{"success is green", domain.StateSuccess, 0x36a64f},
		{"failure is red", domain.StateFailure, 0xe01e5a},
		{"error is red", domain.StateError, 0xe01e5a},
		{"running is yellow", domain.StateRunning, 0xdaa038},
		{"pending is gray", domain.StatePending, 0xaaaaaa},
		{"canceled is gray", domain.StateCanceled, 0xaaaaaa},
		{"unknown state is gray", domain.State("unknown"), 0xaaaaaa},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := colorFor(tt.state); got != tt.want {
				t.Errorf("colorFor(%v) = %#x, want %#x", tt.state, got, tt.want)
			}
		})
	}
}

func TestTemplateFile_Valid(t *testing.T) {
	srv, _ := newRecordingServer(t)
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "template.txt")
	if err := os.WriteFile(templatePath, []byte("Build {{.State}} for {{.RunName}}"), 0600); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}
	n, err := New(Config{WebhookURL: webhookURL(srv.URL), Template: templatePath}, nil)
	if err != nil {
		t.Fatalf("New() with valid template file failed: %v", err)
	}
	if n.tmpl == nil {
		t.Fatal("expected template to be loaded")
	}
}

func TestTemplateFile_Missing(t *testing.T) {
	srv, _ := newRecordingServer(t)
	_, err := New(Config{WebhookURL: webhookURL(srv.URL), Template: "/nonexistent/template.txt"}, nil)
	if err == nil {
		t.Fatal("expected error for missing template file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read template file") {
		t.Errorf("error = %v, want 'failed to read template file'", err)
	}
}

func TestTemplateFile_InvalidSyntax(t *testing.T) {
	srv, _ := newRecordingServer(t)
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "invalid.txt")
	if err := os.WriteFile(templatePath, []byte("{{.Invalid"), 0600); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}
	_, err := New(Config{WebhookURL: webhookURL(srv.URL), Template: templatePath}, nil)
	if err == nil {
		t.Fatal("expected error for invalid template syntax, got nil")
	}
	if !strings.Contains(err.Error(), "invalid template") {
		t.Errorf("error = %v, want 'invalid template'", err)
	}
}

// bodyRecordingServer captures request bodies for payload inspection.
type bodyRecordingServer struct {
	mu   sync.Mutex
	bods []string
}

func newBodyRecordingServer(t *testing.T) (*httptest.Server, *bodyRecordingServer) {
	t.Helper()
	rec := &bodyRecordingServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.bods = append(rec.bods, string(body))
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg-1"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestRoleMention_Webhook_SingleRole(t *testing.T) {
	srv, rec := newBodyRecordingServer(t)
	n, err := New(Config{
		WebhookURL:   webhookURL(srv.URL),
		MentionRoles: []string{"111222333"},
	}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := n.Handle(context.Background(), successEvent()); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.bods) != 1 {
		t.Fatalf("expected 1 request, got %d", len(rec.bods))
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(rec.bods[0]), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	content, _ := payload["content"].(string)
	if !strings.Contains(content, "<@&111222333>") {
		t.Errorf("content = %q, want to contain <@&111222333>", content)
	}
}

func TestRoleMention_Webhook_MultipleRoles(t *testing.T) {
	srv, rec := newBodyRecordingServer(t)
	n, err := New(Config{
		WebhookURL:   webhookURL(srv.URL),
		MentionRoles: []string{"111", "222", "333"},
	}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := n.Handle(context.Background(), successEvent()); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.bods) != 1 {
		t.Fatalf("expected 1 request, got %d", len(rec.bods))
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(rec.bods[0]), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	content, _ := payload["content"].(string)
	want := "<@&111> <@&222> <@&333>"
	if !strings.Contains(content, want) {
		t.Errorf("content = %q, want to contain %q", content, want)
	}
}

func TestRoleMention_Webhook_EmptyRoles_NoContentChange(t *testing.T) {
	srv, rec := newBodyRecordingServer(t)
	n, err := New(Config{
		WebhookURL: webhookURL(srv.URL),
	}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := n.Handle(context.Background(), successEvent()); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.bods) != 1 {
		t.Fatalf("expected 1 request, got %d", len(rec.bods))
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(rec.bods[0]), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	content, _ := payload["content"].(string)
	if strings.Contains(content, "<@&") {
		t.Errorf("content = %q, should not contain role mentions when MentionRoles is empty", content)
	}
}

func TestRoleMention_BotMode_SingleRole(t *testing.T) {
	srv, rec := newBodyRecordingServer(t)
	n, err := New(Config{
		BotToken:     staticRefresher{tok: "bot-test-token"},
		ChannelID:    testChannelID,
		MentionRoles: []string{"999888777"},
		httpClient:   redirectClient(srv.URL),
	}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := n.Handle(context.Background(), successEvent()); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.bods) != 1 {
		t.Fatalf("expected 1 request, got %d", len(rec.bods))
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(rec.bods[0]), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	content, _ := payload["content"].(string)
	if !strings.Contains(content, "<@&999888777>") {
		t.Errorf("content = %q, want to contain <@&999888777>", content)
	}
}

func TestRoleMention_WithTemplate(t *testing.T) {
	srv, rec := newBodyRecordingServer(t)
	n, err := New(Config{
		WebhookURL:   webhookURL(srv.URL),
		Template:     "Build {{.State}} for {{.RunName}}",
		MentionRoles: []string{"555"},
	}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := n.Handle(context.Background(), successEvent()); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.bods) != 1 {
		t.Fatalf("expected 1 request, got %d", len(rec.bods))
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(rec.bods[0]), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	content, _ := payload["content"].(string)
	if !strings.Contains(content, "<@&555>") {
		t.Errorf("content = %q, want to contain <@&555>", content)
	}
	if !strings.Contains(content, "Build success for run-1") {
		t.Errorf("content = %q, want to contain template output", content)
	}
	if !strings.HasPrefix(content, "<@&555>") {
		t.Errorf("content = %q, want role mention before template output", content)
	}
}

func TestDiscordThreadReply(t *testing.T) {
	srv, rec := newRecordingServer(t)
	store := msgstore.NewMemoryStore(0, 0)
	cfg := Config{
		BotToken:     staticRefresher{tok: "bot-test-token"},
		ChannelID:    testChannelID,
		ThreadMode:   ThreadModeGrouped,
		MessageStore: store,
		httpClient:   redirectClient(srv.URL),
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := successEvent()

	// First call posts top-level and stores the message ID.
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("first Handle() failed: %v", err)
	}
	if id, ok := store.Load(testRunID); !ok || id != "msg-1" {
		t.Errorf("stored id = %q, ok=%v; want msg-1", id, ok)
	}

	// Second call for the same RunID must reply in the thread.
	evt.State = domain.StateRunning
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("second Handle() failed: %v", err)
	}

	if len(rec.paths) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(rec.paths))
	}
	// Both calls should be channel messages (POST).
	if rec.methods[0] != http.MethodPost || rec.methods[1] != http.MethodPost {
		t.Errorf("methods = %v, want [POST POST]", rec.methods)
	}
}

func TestDiscordThreadReply_WebhookFailsOpenWithoutStore(t *testing.T) {
	srv, rec := newRecordingServer(t)
	cfg := Config{
		WebhookURL: webhookURL(srv.URL),
		ThreadMode: ThreadModeGrouped,
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := successEvent()
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("first Handle() failed: %v", err)
	}
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("second Handle() failed: %v", err)
	}

	// Without a store, thread mode degrades to normal sends.
	if len(rec.methods) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(rec.methods))
	}
	for i, m := range rec.methods {
		if m != http.MethodPost {
			t.Errorf("call %d method = %q, want POST", i, m)
		}
	}
}
