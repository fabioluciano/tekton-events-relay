package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	testPipelineRelay = "tekton-events-relay"
	testURL           = "https://test"
	testRunID         = "run-123"
	testBuild         = "test/build"
	testNamespace     = "default"
	testFieldCommit   = "Commit"
	testFieldDuration = "Duration"
)

func TestNew(t *testing.T) {
	cfg := Config{WebhookURL: "https://hooks.slack.com/test"}
	n := New(cfg)

	if n == nil {
		t.Fatal("expected notifier, got nil")
	}
	if len(n.cfg.NotifyOn) == 0 {
		t.Error("expected default NotifyOn values")
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
		NotifyOn:   []string{stateError, stateFailure},
		Username:   "custom-bot",
		IconEmoji:  ":custom:",
	}
	n := New(cfg)

	if n.cfg.Channel != "#custom-channel" {
		t.Errorf("channel = %q, want #custom-channel", n.cfg.Channel)
	}
	if len(n.cfg.NotifyOn) != 2 {
		t.Errorf("NotifyOn length = %d, want 2", len(n.cfg.NotifyOn))
	}
	if n.cfg.Username != "custom-bot" {
		t.Errorf("username = %q, want custom-bot", n.cfg.Username)
	}
	if n.cfg.IconEmoji != ":custom:" {
		t.Errorf("icon = %q, want :custom:", n.cfg.IconEmoji)
	}
}

func TestName(t *testing.T) {
	n := New(Config{WebhookURL: testURL})
	if n.Name() != "slack" {
		t.Errorf("Name() = %q, want slack", n.Name())
	}
}

func TestNotify_Success(t *testing.T) {
	var receivedPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		WebhookURL: server.URL,
		NotifyOn:   []string{stateSuccess},
	}
	n := New(cfg)

	evt := domain.Event{
		RunID: testRunID,

		RunName: testRunID,
		State:     domain.StateSuccess,
		Context:   testBuild,
		Namespace: testNamespace,
	}

	err := n.Notify(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if receivedPayload == nil {
		t.Fatal("expected payload to be sent")
	}
}

func TestNotify_FilterByState(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		WebhookURL: server.URL,
		NotifyOn:   []string{stateFailure},
	}
	n := New(cfg)

	evt := domain.Event{
		RunID: testRunID,

		RunName: testRunID,
		State: domain.StateSuccess,
	}

	err := n.Notify(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if callCount != 0 {
		t.Error("webhook should not be called for filtered state")
	}
}

func TestNotify_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := Config{
		WebhookURL: server.URL,
		NotifyOn:   []string{stateSuccess},
	}
	n := New(cfg)

	evt := domain.Event{
		RunID: testRunID,

		RunName: testRunID,
		State:     domain.StateSuccess,
		Context:   testBuild,
		Namespace: testNamespace,
	}

	err := n.Notify(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
}

func TestPayload(t *testing.T) {
	n := New(Config{
		WebhookURL: testURL,
		Username:   "test-bot",
		IconEmoji:  ":test:",
	})

	evt := domain.Event{
		RunID: testRunID,

		RunName: testRunID,
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

	data, ok := payload.(map[string]interface{})
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
	n := New(Config{
		WebhookURL: testURL,
		Channel:    "#alerts",
	})

	evt := domain.Event{
		RunID: "run-456",

		RunName: "run-456",
		State:     domain.StateFailure,
		Context:   "test/deploy",
		Namespace: "prod",
	}

	payload, err := n.payload(evt)
	if err != nil {
		t.Fatalf("payload() error: %v", err)
	}

	data := payload.(map[string]interface{})
	if data["channel"] != "#alerts" {
		t.Errorf("channel = %v, want #alerts", data["channel"])
	}
}

func TestPayload_WithTargetURL(t *testing.T) {
	n := New(Config{WebhookURL: testURL})

	evt := domain.Event{
		RunID: "run-789",

		RunName: "run-789",
		State:     domain.StateSuccess,
		Context:   "ci/build",
		Namespace: "default",
		TargetURL: "https://dashboard.example.com/run/789",
	}

	payload, _ := n.payload(evt)
	data := payload.(map[string]interface{})

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
			RunID: testRunID,

			RunName: testRunID,
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
			RunID: testRunID,

			RunName: testRunID,
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
			RunID: testRunID,

			RunName: testRunID,
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
			RunID: testRunID,

			RunName: testRunID,
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
			RunID: testRunID,

			RunName: testRunID,
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


func TestNotify_MultipleStates(t *testing.T) {
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
				NotifyOn:   []string{string(state)},
			}
			n := New(cfg)

			evt := domain.Event{
				RunID: testRunID,

				RunName: testRunID,
				State:       state,
				Context:     testBuild,
				Namespace:   testNamespace,
				Description: "test description",
			}

			err := n.Notify(context.Background(), evt)
			if err != nil {
				t.Fatalf("Notify() error: %v", err)
			}

			if callCount != 1 {
				t.Errorf("expected 1 webhook call, got %d", callCount)
			}
		})
	}
}
