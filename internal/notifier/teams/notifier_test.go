package teams

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
)

const (
	testSuccess   = "success"
	testRunID     = "test-run-123"
	testNamespace = "default"
	testContext   = "Build"
)

const (
	testWebhookURL = "https://test"
	stateSuccess   = "success"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		wantURL  string
		wantNLen int
	}{
		{
			name:     "with default NotifyOn",
			cfg:      Config{WebhookURL: testWebhookURL},
			wantURL:  testWebhookURL,
			wantNLen: 3,
		},
		{
			name:     "with custom NotifyOn",
			cfg:      Config{WebhookURL: "https://custom", NotifyOn: []string{failureState}},
			wantURL:  "https://custom",
			wantNLen: 1,
		},
		{
			name:     "with empty NotifyOn gets defaults",
			cfg:      Config{WebhookURL: "https://empty", NotifyOn: []string{}},
			wantURL:  "https://empty",
			wantNLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := New(tt.cfg)
			if n == nil {
				t.Fatal("expected notifier")
			}
			if n.cfg.WebhookURL != tt.wantURL {
				t.Errorf("WebhookURL = %q, want %q", n.cfg.WebhookURL, tt.wantURL)
			}
			if len(n.cfg.NotifyOn) != tt.wantNLen {
				t.Errorf("len(NotifyOn) = %d, want %d", len(n.cfg.NotifyOn), tt.wantNLen)
			}
			if n.base == nil {
				t.Error("expected base to be initialized")
			}
			if n.base.HTTP == nil {
				t.Error("expected HTTP client to be initialized")
			}
			if n.base.UserAgent != notifier.UserAgent {
				t.Errorf("UserAgent = %q, want %s", n.base.UserAgent, notifier.UserAgent)
			}
		})
	}
}

func TestName(t *testing.T) {
	n := New(Config{WebhookURL: testWebhookURL})
	if got := n.Name(); got != "teams" {
		t.Errorf("Name() = %q, want teams", got)
	}
}

func TestNotify_Success(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost {
			t.Errorf("Method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if ua := r.Header.Get("User-Agent"); ua != notifier.UserAgent {
			t.Errorf("User-Agent = %q, want %s", ua, notifier.UserAgent)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		if payload[fieldType] != fieldMessage {
			t.Errorf("%s = %v, want %s", fieldType, payload[fieldType], fieldMessage)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := New(Config{
		WebhookURL: server.URL,
		NotifyOn:   []string{stateSuccess},
	})

	event := domain.Event{
		State:       domain.StateSuccess,
		RunID: testRunID,

		RunName: testRunID,
		Namespace:   testNamespace,
		Context:     testContext,
		Description: "Build completed successfully",
		CommitSHA:   "abc123def456",
		TargetURL:   "https://example.com/run/123",
	}

	err := n.Notify(context.Background(), event)
	if err != nil {
		t.Fatalf("Notify() error = %v", err)
	}
	if !called {
		t.Error("webhook was not called")
	}
}

func TestNotify_FilteredState(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := New(Config{
		WebhookURL: server.URL,
		NotifyOn:   []string{failureState},
	})

	event := domain.Event{
		State:       domain.StateSuccess,
		RunID: testRunID,

		RunName: testRunID,
		Namespace:   testNamespace,
		Context:     testContext,
		Description: "Build completed",
	}

	err := n.Notify(context.Background(), event)
	if err != nil {
		t.Fatalf("Notify() error = %v", err)
	}
	if called {
		t.Error("webhook should not have been called for filtered state")
	}
}

func TestNotify_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	n := New(Config{
		WebhookURL: server.URL,
		NotifyOn:   []string{failureState},
	})

	event := domain.Event{
		State:       domain.StateFailure,
		RunID: "test-run-456",

		RunName: "test-run-456",
		Namespace:   testNamespace,
		Context:     testContext,
		Description: "Build failed",
	}

	err := n.Notify(context.Background(), event)
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

//nolint:gocyclo,nestif // Table-driven test with many cases, acceptable complexity
func TestPayload(t *testing.T) {
	tests := []struct {
		name  string
		event domain.Event
	}{
		{
			name: "success with commit",
			event: domain.Event{
				State:       domain.StateSuccess,
				RunID: "run-123",

				RunName: "run-123",
				Namespace:   "prod",
				Context:     "CI Build",
				Description: "Build passed",
				CommitSHA:   "abc123def456789",
				TargetURL:   "https://example.com/run/123",
			},
		},
		{
			name: "failure without commit",
			event: domain.Event{
				State:       domain.StateFailure,
				RunID: "run-456",

				RunName: "run-456",
				Namespace:   "dev",
				Context:     "Test Suite",
				Description: "Tests failed",
				TargetURL:   "https://example.com/run/456",
			},
		},
		{
			name: "error without target url",
			event: domain.Event{
				State:       domain.StateError,
				RunID: "run-789",

				RunName: "run-789",
				Namespace:   "staging",
				Context:     "Deploy",
				Description: "Deployment error",
				CommitSHA:   "short",
			},
		},
		{
			name: "pending state",
			event: domain.Event{
				State:       domain.StatePending,
				RunID: "run-000",

				RunName: "run-000",
				Namespace:   "test",
				Context:     "Queue",
				Description: "Waiting in queue",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := New(Config{WebhookURL: testWebhookURL})
			payload, err := n.payload(tt.event)
			if err != nil {
				t.Fatalf("payload() error = %v", err)
			}

			card, ok := payload.(map[string]any)
			if !ok {
				t.Fatal("payload is not a map")
			}

			if card[fieldType] != fieldMessage {
				t.Errorf("%s = %v, want %s", fieldType, card[fieldType], fieldMessage)
			}

			attachments, ok := card["attachments"].([]map[string]any)
			if !ok || len(attachments) == 0 {
				t.Fatal("attachments missing or invalid")
			}

			attachment := attachments[0]
			if attachment["contentType"] != "application/vnd.microsoft.card.adaptive" {
				t.Errorf("contentType = %v, want application/vnd.microsoft.card.adaptive", attachment["contentType"])
			}

			content, ok := attachment["content"].(map[string]any)
			if !ok {
				t.Fatal("content missing or invalid")
			}

			if content[fieldType] != "AdaptiveCard" {
				t.Errorf("content %s = %v, want AdaptiveCard", fieldType, content[fieldType])
			}
			if content["version"] != "1.4" {
				t.Errorf("version = %v, want 1.4", content["version"])
			}

			body, ok := content["body"].([]map[string]any)
			if !ok || len(body) != 2 {
				t.Fatalf("body missing or invalid, got %d elements", len(body))
			}

			// Check TextBlock
			textBlock := body[0]
			if textBlock[fieldType] != "TextBlock" {
				t.Errorf("textBlock %s = %v, want TextBlock", fieldType, textBlock[fieldType])
			}
			expectedText := "**" + tt.event.Context + "** — " + tt.event.Description
			if textBlock["text"] != expectedText {
				t.Errorf("text = %v, want %v", textBlock["text"], expectedText)
			}

			// Check FactSet
			factSet := body[1]
			if factSet[fieldType] != "FactSet" {
				t.Errorf("factSet %s = %v, want FactSet", fieldType, factSet[fieldType])
			}

			facts, ok := factSet["facts"].([]map[string]string)
			if !ok {
				t.Fatal("facts missing or invalid")
			}

			// Check facts contain State, Run, Namespace
			hasState, hasRun, hasNamespace := false, false, false
			hasCommit := false
			for _, fact := range facts {
				if fact[fieldTitle] == factTitleState && fact[fieldValue] == string(tt.event.State) {
					hasState = true
				}
				if fact[fieldTitle] == "Run" && fact[fieldValue] == tt.event.RunName {
					hasRun = true
				}
				if fact[fieldTitle] == "Namespace" && fact[fieldValue] == tt.event.Namespace {
					hasNamespace = true
				}
				if fact[fieldTitle] == factTitleCommit {
					hasCommit = true
				}
			}

			if !hasState {
				t.Error("State fact missing")
			}
			if !hasRun {
				t.Error("Run fact missing")
			}
			if !hasNamespace {
				t.Error("Namespace fact missing")
			}

			if tt.event.CommitSHA != "" && !hasCommit {
				t.Error("Commit fact missing when CommitSHA present")
			}

			// Check actions when TargetURL present
			if tt.event.TargetURL != "" {
				actions, ok := content["actions"].([]map[string]any)
				if !ok || len(actions) == 0 {
					t.Error("actions missing when TargetURL present")
				} else {
					action := actions[0]
					if action[fieldType] != "Action.OpenUrl" {
						t.Errorf("action %s = %v, want Action.OpenUrl", fieldType, action[fieldType])
					}
					if action["url"] != tt.event.TargetURL {
						t.Errorf("action url = %v, want %v", action["url"], tt.event.TargetURL)
					}
				}
			} else {
				if _, hasActions := content["actions"]; hasActions {
					t.Error("actions should not be present when TargetURL empty")
				}
			}

			// Verify commit SHA is truncated to 7 chars if longer
			if tt.event.CommitSHA != "" && len(tt.event.CommitSHA) > 7 {
				for _, fact := range facts {
					if fact[fieldTitle] == factTitleCommit {
						if len(fact[fieldValue]) != 7 {
							t.Errorf("commit SHA should be truncated to 7 chars, got %q", fact[fieldValue])
						}
						if fact[fieldValue] != tt.event.CommitSHA[:7] {
							t.Errorf("commit SHA = %q, want %q", fact[fieldValue], tt.event.CommitSHA[:7])
						}
					}
				}
			}
		})
	}
}

func TestColorFor(t *testing.T) {
	tests := []struct {
		state domain.State
		want  string
	}{
		{domain.StateSuccess, "Good"},
		{domain.StateFailure, colorAttention},
		{domain.StateError, colorAttention},
		{domain.StatePending, colorDefault},
		{domain.StateRunning, colorDefault},
		{domain.StateCanceled, colorDefault},
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

func TestNotify_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := New(Config{
		WebhookURL: server.URL,
		NotifyOn:   []string{stateSuccess},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	event := domain.Event{
		State:       domain.StateSuccess,
		RunID: testRunID,

		RunName: testRunID,
		Namespace:   testNamespace,
		Context:     testContext,
		Description: "Build completed",
	}

	err := n.Notify(ctx, event)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

func TestNotify_AllStatesIntegration(t *testing.T) {
	states := []domain.State{
		domain.StatePending,
		domain.StateRunning,
		domain.StateSuccess,
		domain.StateFailure,
		domain.StateError,
		domain.StateCanceled,
	}

	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true

				body, _ := io.ReadAll(r.Body)
				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to unmarshal: %v", err)
				}

				attachments := payload["attachments"].([]any)
				attachment := attachments[0].(map[string]any)
				content := attachment["content"].(map[string]any)
				bodyItems := content["body"].([]any)
				factSet := bodyItems[1].(map[string]any)
				facts := factSet["facts"].([]any)

				// Verify state in facts
				for _, f := range facts {
					fact := f.(map[string]any)
					if fact[fieldTitle] == factTitleState && fact[fieldValue] != string(state) {
						t.Errorf("state in payload = %v, want %v", fact[fieldValue], state)
					}
				}

				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			n := New(Config{
				WebhookURL: server.URL,
				NotifyOn:   []string{string(state)},
			})

			event := domain.Event{
				State:       state,
				RunID:       "test-run-" + string(state),
				Namespace:   testNamespace,
				Context:     testContext,
				Description: "Test " + string(state),
			}

			err := n.Notify(context.Background(), event)
			if err != nil {
				t.Fatalf("Notify() error = %v", err)
			}
			if !called {
				t.Error("webhook was not called")
			}
		})
	}
}
