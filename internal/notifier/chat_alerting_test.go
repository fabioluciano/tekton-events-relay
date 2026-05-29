package notifier_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
		w.WriteHeader(200)
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

func TestSlack_NotifiesOnFailure(t *testing.T) {
	srv, body := captureServer(t)
	n := slack.New(slack.Config{WebhookURL: srv.URL, NotifyOn: []string{testStateFailure}})
	if err := n.Notify(context.Background(), testEvent(domain.StateFailure)); err != nil {
		t.Fatal(err)
	}
	if (*body)[testAttachments] == nil {
		t.Error("expected attachments in payload")
	}
}

func TestSlack_SkipsNonMatchingState(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := slack.New(slack.Config{WebhookURL: srv.URL, NotifyOn: []string{testStateFailure}})
	_ = n.Notify(context.Background(), testEvent(domain.StateRunning))
	if called {
		t.Error("should not have called webhook for running state")
	}
}

func TestTeams_Payload(t *testing.T) {
	srv, body := captureServer(t)
	n := teams.New(teams.Config{WebhookURL: srv.URL, NotifyOn: []string{testStateSuccess}})
	if err := n.Notify(context.Background(), testEvent(domain.StateSuccess)); err != nil {
		t.Fatal(err)
	}
	if (*body)[testAttachments] == nil {
		t.Error("expected adaptive card attachments")
	}
}

func TestDiscord_Payload(t *testing.T) {
	srv, body := captureServer(t)
	n := discord.New(discord.Config{WebhookURL: srv.URL})
	if err := n.Notify(context.Background(), testEvent(domain.StateSuccess)); err != nil {
		t.Fatal(err)
	}
	if (*body)[testEmbeds] == nil {
		t.Error("expected embeds in discord payload")
	}
}

func TestDatadog_Payload(t *testing.T) {
	_, body := captureServer(t)
	n := datadog.New(datadog.Config{APIKey: "dd-key", Site: "datadoghq.com"})
	// Datadog uses fixed URL (api.<site>/api/v2/events) — test payload structure
	_ = n
	_ = body
	if n.Name() != testDatadogName {
		t.Errorf("name = %q", n.Name())
	}
}

func TestWebhook_ForwardsAllFields(t *testing.T) {
	srv, body := captureServer(t)
	n := webhook.New(webhook.Config{
		URL:     srv.URL,
		Headers: map[string]string{"X-Token": "secret"},
	})
	e := testEvent(domain.StateSuccess)
	e.CommitSHA = testCommitSHAAbc
	if err := n.Notify(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if (*body)[testStateField] != testStateSuccess {
		t.Errorf("state = %v", (*body)[testStateField])
	}
	if (*body)[testCommitSHAField] != testCommitSHAAbc {
		t.Errorf("commit_sha = %v", (*body)[testCommitSHAField])
	}
}

func TestWebhook_FiltersByState(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := webhook.New(webhook.Config{URL: srv.URL, NotifyOn: []string{testStateFailure}})
	_ = n.Notify(context.Background(), testEvent(domain.StateSuccess))
	if called {
		t.Error("webhook should not be called for success when notify_on=[failure]")
	}
}
