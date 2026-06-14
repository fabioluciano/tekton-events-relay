// Package teams implements the Notifier for Microsoft Teams via Incoming Webhooks.
// Uses the Adaptive Card format.
// Doc: https://learn.microsoft.com/microsoftteams/platform/webhooks-and-connectors/how-to/add-incoming-webhook
package teams

import (
	"go.uber.org/zap"

	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
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
	Template   string // optional Go template; if empty, uses default format
}

// Notifier implements the notifier for Microsoft Teams Incoming Webhooks.
type Notifier struct {
	base *notifier.Base
	cfg  Config
	tmpl *template.Template
}

// New creates a new Teams notifier with the given configuration.
func New(cfg Config, log *zap.Logger) (*Notifier, error) {
	n := &Notifier{cfg: cfg}

	// Resolve the template: inline string or an /etc/templates/... path
	// (the chart renders configmap defaults / configmapRef as a path).
	templateContent, err := scm.LoadTemplateString(cfg.Template)
	if err != nil {
		return nil, fmt.Errorf("load template: %w", err)
	}

	if templateContent != "" {
		tmpl, err := template.New("teams").Parse(templateContent)
		if err != nil {
			return nil, fmt.Errorf("invalid template: %w", err)
		}
		n.tmpl = tmpl
	}

	n.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildURL:     func(_ domain.Event) (string, error) { return cfg.WebhookURL, nil },
		BuildPayload: n.payload,
		UserAgent:    notifier.UserAgent,
		Log:          log,
	}
	return n, nil
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "teams" }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle sends the event to Teams.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	return n.base.Send(ctx, e)
}

func (n *Notifier) payload(e domain.Event) (any, error) {
	// Use custom template if available
	if n.tmpl != nil {
		var buf bytes.Buffer
		if err := n.tmpl.Execute(&buf, e); err != nil {
			return nil, fmt.Errorf("template execution failed: %w", err)
		}

		return map[string]any{
			fieldType: fieldMessage,
			"text":    buf.String(),
		}, nil
	}

	// Use default format
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
		attachments := card["attachments"].([]map[string]any)
		content := attachments[0]["content"].(map[string]any)
		content["actions"] = []map[string]any{
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
