// Package teams implements the Notifier for Microsoft Teams via Incoming Webhooks.
// Uses the Adaptive Card format.
// Doc: https://learn.microsoft.com/microsoftteams/platform/webhooks-and-connectors/how-to/add-incoming-webhook
package teams

import (
	"go.uber.org/zap"

	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"text/template"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
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

// MentionEntry represents a user to @-mention in the Adaptive Card.
type MentionEntry struct {
	Name string
	ID   string
}

// Config holds the configuration for the Teams notifier.
type Config struct {
	WebhookURL   string
	Template     string // optional Go template; if empty, uses default format
	MentionUsers []MentionEntry
	// HTTPClient overrides the HTTP client. When nil, notifier.DefaultHTTPClient() is used.
	HTTPClient *http.Client
	// RetryPolicy overrides the global retry policy. When nil, the global default is used.
	RetryPolicy *httpx.RetryPolicy
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

	httpClient := notifier.DefaultHTTPClient()
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}
	n.base = &notifier.Base{
		HTTP:         httpClient,
		BuildURL:     func(_ domain.Event) (string, error) { return cfg.WebhookURL, nil },
		BuildPayload: n.payload,
		UserAgent:    notifier.UserAgent,
		Log:          log,
		RetryPolicy:  cfg.RetryPolicy,
	}
	return n, nil
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "teams" }

// Provider returns the provider type identifier.
func (n *Notifier) Provider() string { return "teams" }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle sends the event to Teams.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	return n.base.Send(ctx, e)
}

// Close is a no-op; the Teams notifier holds no closable resources.
func (n *Notifier) Close() error { return nil }

// Flush sends multiple events as a single combined Teams Adaptive Card.
// Each event becomes a TextBlock + FactSet pair in the card body.
func (n *Notifier) Flush(ctx context.Context, events []domain.Event) error {
	if len(events) == 0 {
		return nil
	}

	body := make([]map[string]any, 0, len(events)*2)
	for _, e := range events {
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

		body = append(body,
			map[string]any{
				fieldType: "TextBlock",
				"text":    fmt.Sprintf("**%s** — %s", e.Context, e.Description),
				"wrap":    true,
				"color":   color,
				"weight":  "Bolder",
			},
			map[string]any{
				fieldType: "FactSet",
				"facts":   facts,
			},
		)
		if len(events) > 1 {
			body = append(body, map[string]any{
				fieldType: "Separator",
			})
		}
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

	url, err := n.base.BuildURL(events[0])
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	payload, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("marshal batch payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", notifier.UserAgent)

	rp := httpx.DefaultRetryPolicy()
	if n.base.RetryPolicy != nil {
		rp = *n.base.RetryPolicy
	}
	resp, err := httpx.DoWithRetryPolicy(n.base.HTTP, req, rp)
	if err != nil {
		return fmt.Errorf("teams batch webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("teams batch webhook responded %d: %s", resp.StatusCode, string(buf))
	}
	return nil
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

	if len(n.cfg.MentionUsers) > 0 {
		attachments := card["attachments"].([]map[string]any)
		content := attachments[0]["content"].(map[string]any)
		entities := make([]map[string]any, 0, len(n.cfg.MentionUsers))
		for _, mu := range n.cfg.MentionUsers {
			entities = append(entities, map[string]any{
				fieldType: "mention",
				"text":    fmt.Sprintf("<at>%s</at>", mu.Name),
				"mentioned": map[string]any{
					"id":   mu.ID,
					"name": mu.Name,
				},
			})
		}
		content["msteams"] = map[string]any{
			"entities": entities,
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
