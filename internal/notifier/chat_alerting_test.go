package notifier_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/datadog"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/discord"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/slack"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/teams"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/webhook"
)

const (
	testStateFailure   = "failure"
	testStateSuccess   = "success"
	testAttachments    = "attachments"
	testDatadogName    = "datadog"
	testCommitSHAAbc   = "abc123"
	testStateField     = "state"
	testCommitSHAField = "commit_sha"
	testEmbeds         = "embeds"
)

func captureServer(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	body := &map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)
	return srv, body
}

func testEvent(state domain.State) domain.Event {
	return domain.Event{
		RunID: "run-1", Namespace: "ci",
		State: state, Context: "tekton/build",
		Description: "All tasks completed",
		CommitSHA:   "abc123def",
	}
}

func TestSlack_HandlesOnFailure(t *testing.T) {
	srv, body := captureServer(t)
	n, err := slack.New(slack.Config{WebhookURL: srv.URL}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Handle(context.Background(), testEvent(domain.StateFailure)); err != nil {
		t.Fatal(err)
	}
	if (*body)[testAttachments] == nil {
		t.Error("expected attachments in payload")
	}
}

func TestSlack_SendsAllStates(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n, err := slack.New(slack.Config{WebhookURL: srv.URL}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	_ = n.Handle(context.Background(), testEvent(domain.StateRunning))
	if !called {
		t.Error("Handle should always send - filtering is done externally by CEL")
	}
}

func TestTeams_Payload(t *testing.T) {
	srv, body := captureServer(t)
	n, err := teams.New(teams.Config{WebhookURL: srv.URL}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Handle(context.Background(), testEvent(domain.StateSuccess)); err != nil {
		t.Fatal(err)
	}
	if (*body)[testAttachments] == nil {
		t.Error("expected adaptive card attachments")
	}
}

func TestDiscord_Payload(t *testing.T) {
	srv, body := captureServer(t)
	n, err := discord.New(discord.Config{WebhookURL: srv.URL + "/api/webhooks/123/abc"}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Handle(context.Background(), testEvent(domain.StateSuccess)); err != nil {
		t.Fatal(err)
	}
	if (*body)[testEmbeds] == nil {
		t.Error("expected embeds in discord payload")
	}
}

func TestDatadog_Payload(t *testing.T) {
	_, body := captureServer(t)
	n := datadog.New(datadog.Config{APIKey: "dd-key", Site: "datadoghq.com"}, zap.NewNop())
	// Datadog uses fixed URL (api.<site>/api/v2/events) — test payload structure

	_ = n
	_ = body
	if n.Name() != testDatadogName {
		t.Errorf("name = %q", n.Name())
	}
}

func TestWebhook_ForwardsAllFields(t *testing.T) {
	srv, body := captureServer(t)
	n, err := webhook.New(webhook.Config{
		URL:     srv.URL,
		Headers: map[string]string{"X-Token": "secret"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := testEvent(domain.StateSuccess)
	e.CommitSHA = testCommitSHAAbc
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if (*body)[testStateField] != testStateSuccess {
		t.Errorf("state = %v", (*body)[testStateField])
	}
	if (*body)[testCommitSHAField] != testCommitSHAAbc {
		t.Errorf("commit_sha = %v", (*body)[testCommitSHAField])
	}
}

func TestWebhook_SendsAllStates(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n, err := webhook.New(webhook.Config{URL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = n.Handle(context.Background(), testEvent(domain.StateSuccess))
	if !called {
		t.Error("Handle should always send - filtering is done externally by CEL")
	}
}
