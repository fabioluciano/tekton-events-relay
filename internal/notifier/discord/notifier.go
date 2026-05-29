// Package discord implements the Notifier for Discord via Webhook.
// Doc: https://discord.com/developers/docs/resources/webhook#execute-webhook
package discord

import (
	"context"
	"fmt"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	defaultUsername = "tekton-events-relay"
	stateError      = "error"
	stateSuccess    = "success"
	stateFailure    = "failure"
	fieldInline     = "inline"
	fieldCommit     = "Commit"
	fieldName       = "name"
	fieldValue      = "value"
)

// Config holds the configuration for the Discord notifier.
type Config struct {
	WebhookURL string
	Username   string   // default: tekton-events-relay
	NotifyOn   []string // default: failure, error, success
}

// Notifier sends events to Discord via webhook.
type Notifier struct {
	base *notifier.Base
	cfg  Config
}

func New(cfg Config) *Notifier {
	if len(cfg.NotifyOn) == 0 {
		cfg.NotifyOn = []string{stateFailure, stateError, stateSuccess}
	}
	if cfg.Username == "" {
		cfg.Username = defaultUsername
	}
	n := &Notifier{cfg: cfg}
	n.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildURL:     func(_ domain.Event) (string, error) { return cfg.WebhookURL, nil },
		BuildPayload: n.payload,
		UserAgent:    defaultUsername,
	}
	return n
}

func (n *Notifier) Name() string { return "discord" }

func (n *Notifier) Notify(ctx context.Context, e domain.Event) error {
	if !notifier.ShouldNotify(n.cfg.NotifyOn, e.State) {
		return nil
	}
	return n.base.Send(ctx, e)
}

func (n *Notifier) payload(e domain.Event) (any, error) {
	fields := []map[string]any{
		{fieldName: "State", fieldValue: fmt.Sprintf("`%s`", e.State), fieldInline: true},
		{fieldName: "Run", fieldValue: fmt.Sprintf("`%s`", e.RunName), fieldInline: true},
	}
	if e.CommitSHA != "" {
		sha := e.CommitSHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		fields = append(fields, map[string]any{fieldName: fieldCommit, fieldValue: fmt.Sprintf("`%s`", sha), fieldInline: true})
	}

	embed := map[string]any{
		"title":       e.Context,
		"description": e.Description,
		"color":       colorFor(e.State),
		"fields":      fields,
		"footer":      map[string]string{"text": fmt.Sprintf("%s/%s", e.Namespace, e.RunName)},
	}
	if e.TargetURL != "" {
		embed["url"] = e.TargetURL
	}

	return map[string]any{
		"username": n.cfg.Username,
		"embeds":   []any{embed},
	}, nil
}

func colorFor(s domain.State) int {
	switch s {
	case domain.StateSuccess:
		return 0x36a64f
	case domain.StateFailure, domain.StateError:
		return 0xe01e5a
	case domain.StateRunning:
		return 0xdaa038
	default:
		return 0xaaaaaa
	}
}
