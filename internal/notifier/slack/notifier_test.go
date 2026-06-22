package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/msgstore"
)

const (
	testPipelineRelay = "tekton-events-relay"
	testURL           = "https://test"
	testRunID         = "run-123"
	testBuild         = "test/build"
	testNamespace     = "default"
	testFieldCommit   = "Commit"
	testFieldDuration = "Duration"
	testChannelID     = "C12345"
)

func TestNew(t *testing.T) {
	cfg := Config{WebhookURL: "https://hooks.slack.com/test"}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if n == nil {
		t.Fatal("expected notifier, got nil")
	}
	if n.cfg.Username != testPipelineRelay {
		t.Errorf("username = %q, want tekton-events-relay", n.cfg.Username)
	}
	if n.cfg.IconEmoji != ":rocket:" {
		t.Errorf("icon = %q, want :rocket:", n.cfg.IconEmoji)
	}
}

func TestNew_CustomConfig(t *testing.T) {
	cfg := Config{
		WebhookURL: "https://hooks.slack.com/test",
		Channel:    "#custom-channel",
		Username:   "custom-bot",
		IconEmoji:  ":custom:",
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
	if n.cfg.IconEmoji != ":custom:" {
		t.Errorf("icon = %q, want :custom:", n.cfg.IconEmoji)
	}
}

// staticRefresher returns a fixed token; rotatingRefresher returns a new token
// each call so tests can prove the token is resolved per request, not cached.
type staticRefresher struct{ tok string }

func (s staticRefresher) Token(_ context.Context) (string, error) { return s.tok, nil }

type rotatingRefresher struct{ n int }

func (r *rotatingRefresher) Token(_ context.Context) (string, error) {
	r.n++
	return fmt.Sprintf("xoxb-token-%d", r.n), nil
}

// slackTestServer records each request path and Authorization header, replying
// with a minimal chat.postMessage / chat.update success body.
type slackTestServer struct {
	paths []string
	auths []string
}

func newSlackTestServer(t *testing.T) (*httptest.Server, *slackTestServer) {
	t.Helper()
	rec := &slackTestServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.paths = append(rec.paths, r.URL.Path)
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C12345","ts":"111.111"}`))
	}))
	return srv, rec
}

func TestNew_BotTokenMode(t *testing.T) {
	srv, rec := newSlackTestServer(t)
	defer srv.Close()

	cfg := Config{
		Token:     staticRefresher{tok: "xoxb-test-token"},
		ChannelID: testChannelID,
		apiURL:    srv.URL + "/",
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

	if len(rec.paths) != 1 || !strings.Contains(rec.paths[0], "chat.postMessage") {
		t.Errorf("paths = %v, expected one chat.postMessage call", rec.paths)
	}
	if rec.auths[0] != "Bearer xoxb-test-token" {
		t.Errorf("Authorization = %q, want %q", rec.auths[0], "Bearer xoxb-test-token")
	}
	if n.cfg.Channel != testChannelID {
		t.Errorf("Channel = %q, want C12345", n.cfg.Channel)
	}
}

func TestBotToken_Upsert_SecondCallUpdates(t *testing.T) {
	srv, rec := newSlackTestServer(t)
	defer srv.Close()

	cfg := Config{
		Token:        staticRefresher{tok: "xoxb-test-token"},
		ChannelID:    testChannelID,
		Mode:         ModeUpsert,
		MessageStore: msgstore.NewMemoryStore(0, 0),
		apiURL:       srv.URL + "/",
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{RunID: testRunID, RunName: testRunID, State: domain.StateRunning, Context: testBuild, Namespace: testNamespace}

	// First call posts and stores the ts.
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("first Handle() failed: %v", err)
	}
	// Second call for the same RunID must edit via chat.update.
	evt.State = domain.StateSuccess
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("second Handle() failed: %v", err)
	}

	if len(rec.paths) != 2 {
		t.Fatalf("expected 2 calls, got %d (%v)", len(rec.paths), rec.paths)
	}
	if !strings.Contains(rec.paths[0], "chat.postMessage") {
		t.Errorf("first path = %q, want chat.postMessage", rec.paths[0])
	}
	if !strings.Contains(rec.paths[1], "chat.update") {
		t.Errorf("second path = %q, want chat.update", rec.paths[1])
	}
}

func TestBotToken_Upsert_FailsOpenWithoutStore(t *testing.T) {
	srv, rec := newSlackTestServer(t)
	defer srv.Close()

	cfg := Config{
		Token:     staticRefresher{tok: "xoxb-test-token"},
		ChannelID: testChannelID,
		Mode:      ModeUpsert,
		// No MessageStore: upsert must degrade to a plain post each time.
		apiURL: srv.URL + "/",
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{RunID: testRunID, RunName: testRunID, State: domain.StateRunning, Context: testBuild, Namespace: testNamespace}
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("first Handle() failed: %v", err)
	}
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("second Handle() failed: %v", err)
	}

	if len(rec.paths) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(rec.paths))
	}
	for i, p := range rec.paths {
		if !strings.Contains(p, "chat.postMessage") {
			t.Errorf("call %d path = %q, want chat.postMessage (fail-open to post)", i, p)
		}
	}
}

func TestBotToken_TokenRefreshedPerRequest(t *testing.T) {
	srv, rec := newSlackTestServer(t)
	defer srv.Close()

	cfg := Config{
		Token:     &rotatingRefresher{},
		ChannelID: testChannelID,
		apiURL:    srv.URL + "/",
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{RunID: testRunID, RunName: testRunID, State: domain.StateSuccess, Context: testBuild, Namespace: testNamespace}
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
	if rec.auths[0] != "Bearer xoxb-token-1" || rec.auths[1] != "Bearer xoxb-token-2" {
		t.Errorf("auths = %v, want rotating Bearer tokens", rec.auths)
	}
}

func TestBotToken_ThreadTS_SetsThreadOption(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if got := r.FormValue("thread_ts"); got != "999.999" {
			t.Errorf("thread_ts = %q, want 999.999", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C12345","ts":"111.111"}`))
	}))
	defer srv.Close()

	cfg := Config{
		Token:     staticRefresher{tok: "xoxb-test-token"},
		ChannelID: testChannelID,
		ThreadTS:  "999.999",
		apiURL:    srv.URL + "/",
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{RunID: testRunID, RunName: testRunID, State: domain.StateSuccess, Context: testBuild, Namespace: testNamespace}
	if err := n.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle() failed: %v", err)
	}
}

func TestName(t *testing.T) {
	n, err := New(Config{WebhookURL: testURL}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if n.Name() != "slack" {
		t.Errorf("Name() = %q, want slack", n.Name())
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

func TestHandle_Success(t *testing.T) {
	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		WebhookURL: server.URL,
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

	err = n.Handle(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if receivedPayload == nil {
		t.Fatal("expected payload to be sent")
	}
}

func TestHandle_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := Config{
		WebhookURL: server.URL,
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

	err = n.Handle(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
}

func TestTemplateFile_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "template.txt")
	if err := os.WriteFile(templatePath, []byte("Build {{.State}} for {{.RunName}}"), 0600); err != nil {
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
	templatePath := filepath.Join(tmpDir, "invalid.txt")
	if err := os.WriteFile(templatePath, []byte("{{.Invalid"), 0600); err != nil {
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

func TestPayload(t *testing.T) {
	n, err := New(Config{
		WebhookURL: testURL,
		Username:   "test-bot",
		IconEmoji:  ":test:",
	}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{
		RunID:     testRunID,
		RunName:   testRunID,
		State:     domain.StateSuccess,
		Context:   "tekton/build",
		Namespace: testNamespace,
	}

	payload, err := n.payload(evt)
	if err != nil {
		t.Fatalf("payload() error: %v", err)
	}

	if payload == nil {
		t.Fatal("expected payload, got nil")
	}

	data, ok := payload.(map[string]any)
	if !ok {
		t.Fatal("payload should be a map")
	}

	if data["username"] != "test-bot" {
		t.Errorf("username = %v, want test-bot", data["username"])
	}
	if data["icon_emoji"] != ":test:" {
		t.Errorf("icon_emoji = %v, want :test:", data["icon_emoji"])
	}
}

func TestPayload_WithChannel(t *testing.T) {
	n, err := New(Config{
		WebhookURL: testURL,
		Channel:    "#alerts",
	}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{
		RunID:     "run-456",
		RunName:   "run-456",
		State:     domain.StateFailure,
		Context:   "test/deploy",
		Namespace: "prod",
	}

	payload, err := n.payload(evt)
	if err != nil {
		t.Fatalf("payload() error: %v", err)
	}

	data := payload.(map[string]any)
	if data["channel"] != "#alerts" {
		t.Errorf("channel = %v, want #alerts", data["channel"])
	}
}

func TestPayload_WithTargetURL(t *testing.T) {
	n, err := New(Config{WebhookURL: testURL}, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{
		RunID:     "run-789",
		RunName:   "run-789",
		State:     domain.StateSuccess,
		Context:   "ci/build",
		Namespace: "default",
		TargetURL: "https://dashboard.example.com/run/789",
	}

	payload, _ := n.payload(evt)
	data := payload.(map[string]any)

	attachments := data["attachments"].([]map[string]any)
	text := attachments[0]["text"].(string)

	if !strings.Contains(text, "View run") {
		t.Error("expected 'View run' link in text when TargetURL is set")
	}
	if !strings.Contains(text, evt.TargetURL) {
		t.Error("expected TargetURL in text")
	}
}

func TestColorFor(t *testing.T) {
	tests := []struct {
		state domain.State
		want  string
	}{
		{domain.StateSuccess, "#36a64f"},
		{domain.StateFailure, colorFailure},
		{domain.StateError, colorFailure},
		{domain.StateRunning, "#daa038"},
		{domain.StatePending, colorUnknown},
		{domain.StateCanceled, colorUnknown},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := colorFor(tt.state)
			if got != tt.want {
				t.Errorf("colorFor(%v) = %q, want %q", tt.state, got, tt.want)
			}
		})
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

func TestFields(t *testing.T) {
	t.Run("basic fields", func(t *testing.T) {
		evt := domain.Event{
			RunID:     testRunID,
			RunName:   testRunID,
			State:     domain.StateSuccess,
			Namespace: testNamespace,
		}

		fields := fields(evt)

		if len(fields) != 2 {
			t.Errorf("expected 2 fields, got %d", len(fields))
		}

		if fields[0]["title"] != "State" {
			t.Errorf("first field title = %v, want State", fields[0]["title"])
		}
		if fields[1]["title"] != "Run" {
			t.Errorf("second field title = %v, want Run", fields[1]["title"])
		}
	})

	t.Run("with commit SHA", func(t *testing.T) {
		evt := domain.Event{
			RunID:     testRunID,
			RunName:   testRunID,
			State:     domain.StateSuccess,
			Namespace: testNamespace,
			CommitSHA: "abcdef1234567890",
		}

		fields := fields(evt)

		if len(fields) != 3 {
			t.Errorf("expected 3 fields, got %d", len(fields))
		}

		found := false
		for _, f := range fields {
			if f["title"] == testFieldCommit {
				found = true
				if f["value"] != "abcdef1" {
					t.Errorf("commit value = %v, want abcdef1", f["value"])
				}
			}
		}
		if !found {
			t.Error("expected Commit field")
		}
	})

	t.Run("with short commit SHA", func(t *testing.T) {
		evt := domain.Event{
			RunID:     testRunID,
			RunName:   testRunID,
			State:     domain.StateSuccess,
			Namespace: testNamespace,
			CommitSHA: "abc123",
		}

		fields := fields(evt)

		for _, f := range fields {
			if f["title"] == testFieldCommit {
				if f["value"] != "abc123" {
					t.Errorf("commit value = %v, want abc123", f["value"])
				}
			}
		}
	})

	t.Run("with duration", func(t *testing.T) {
		start := time.Now()
		end := start.Add(5 * time.Minute)

		evt := domain.Event{
			RunID:      testRunID,
			RunName:    testRunID,
			State:      domain.StateSuccess,
			Namespace:  testNamespace,
			StartedAt:  start,
			FinishedAt: end,
		}

		fields := fields(evt)

		found := false
		for _, f := range fields {
			if f["title"] == testFieldDuration {
				found = true
				val := f["value"].(string)
				if !strings.Contains(val, "5m") {
					t.Errorf("duration value = %v, expected to contain 5m", val)
				}
			}
		}
		if !found {
			t.Error("expected Duration field")
		}
	})

	t.Run("without duration when times not set", func(t *testing.T) {
		evt := domain.Event{
			RunID:     testRunID,
			RunName:   testRunID,
			State:     domain.StateRunning,
			Namespace: testNamespace,
		}

		fields := fields(evt)

		for _, f := range fields {
			if f["title"] == testFieldDuration {
				t.Error("should not have Duration field when times not set")
			}
		}
	})
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

			cfg := Config{
				WebhookURL: server.URL,
			}
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

			err = n.Handle(context.Background(), evt)
			if err != nil {
				t.Fatalf("Handle() error: %v", err)
			}

			if callCount != 1 {
				t.Errorf("expected 1 webhook call, got %d", callCount)
			}
		})
	}
}
