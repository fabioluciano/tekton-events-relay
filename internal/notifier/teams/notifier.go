// Package teams implements the Notifier for Microsoft Teams via Incoming Webhooks.
// Uses the Adaptive Card format (replaced legacy MessageCard).
// Doc: https://learn.microsoft.com/microsoftteams/platform/webhooks-and-connectors/how-to/add-incoming-webhook
package teams

import (
	"context"
	"fmt"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	failureState    = "failure"
	factTitleState  = "State"
	factTitleCommit = "Commit"
	fieldType       = "type"
	fieldMessage    = "message"
	fieldTitle      = "title"
	fieldValue      = "value"
	colorAttention  = "Attention"
	colorDefault    = "Default"
)

// Config holds the configuration for the Teams notifier.
type Config struct {
	WebhookURL string
	NotifyOn   []string // default: failure, error, success
}

// Notifier implements the notifier for Microsoft Teams Incoming Webhooks.
type Notifier struct {
	base *notifier.Base
	cfg  Config
}

func New(cfg Config) *Notifier {
	if len(cfg.NotifyOn) == 0 {
		cfg.NotifyOn = []string{failureState, "error", "success"}
	}
	n := &Notifier{cfg: cfg}
	n.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildURL:     func(_ domain.Event) (string, error) { return cfg.WebhookURL, nil },
		BuildPayload: n.payload,
		UserAgent:    notifier.UserAgent,
	}
	return n
}

func (n *Notifier) Name() string { return "teams" }

func (n *Notifier) Notify(ctx context.Context, e domain.Event) error {
	if !notifier.ShouldNotify(n.cfg.NotifyOn, e.State) {
		return nil
	}
	return n.base.Send(ctx, e)
}

func (n *Notifier) payload(e domain.Event) (any, error) {
	color := colorFor(e.State)
	facts := []map[string]string{
		{fieldTitle: factTitleState, fieldValue: string(e.State)},
		{fieldTitle: "Run", fieldValue: e.RunName},
		{fieldTitle: "Namespace", fieldValue: e.Namespace},
	}
	if e.CommitSHA != "" {
		short := e.CommitSHA
		if len(short) > 7 {
			short = short[:7]
		}
		facts = append(facts, map[string]string{fieldTitle: factTitleCommit, fieldValue: short})
	}

	body := []map[string]any{
		{
			fieldType: "TextBlock",
			"text":    fmt.Sprintf("**%s** — %s", e.Context, e.Description),
			"wrap":    true,
			"color":   color,
			"weight":  "Bolder",
		},
		{
			fieldType: "FactSet",
			"facts":   facts,
		},
	}

	card := map[string]any{
		fieldType: fieldMessage,
		"attachments": []map[string]any{
			{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content": map[string]any{
					"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
					fieldType: "AdaptiveCard",
					"version": "1.4",
					"body":    body,
				},
			},
		},
	}

	if e.TargetURL != "" {
		card["attachments"].([]map[string]any)[0]["content"].(map[string]any)["actions"] = []map[string]any{
			{fieldType: "Action.OpenUrl", "title": "View run", "url": e.TargetURL},
		}
	}

	return card, nil
}

func colorFor(s domain.State) string {
	switch s {
	case domain.StateSuccess:
		return "Good"
	case domain.StateFailure, domain.StateError:
		return colorAttention
	default:
		return colorDefault
	}
}
