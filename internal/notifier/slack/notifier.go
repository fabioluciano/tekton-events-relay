// Package slack implements the Notifier for Slack via Incoming Webhooks.
// Doc: https://api.slack.com/messaging/webhooks
//
// Filters by State: by default notifies on failure, error and success.
// Configurable via NotifyOn.
package slack

import (
	"go.uber.org/zap"

	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/template"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	fieldKeyShort      = "short"
	fieldKeyTitle      = "title"
	fieldKeyValue      = "value"
	fieldTitleCommit   = "Commit"
	fieldTitleDuration = "Duration"
	colorFailure       = "#e01e5a"
	colorUnknown       = "#aaaaaa"
	emojiFailure       = ":x:"
	emojiUnknown       = ":grey_question:"
)

// Config holds the Slack notifier configuration.
type Config struct {
	// Webhook mode
	WebhookURL string
	// Bot token mode
	BotToken  string
	ChannelID string
	// Common
	Channel      string // optional — override of the channel configured in webhook
	Username     string // displayed name; default: tekton-events-relay
	IconEmoji    string // default: :rocket:
	Template     string // optional Go template; if empty, uses default format
	TemplateFile string // optional path to template file; takes precedence over Template
}

// Notifier implements the notifier for Slack Incoming Webhooks.
type Notifier struct {
	base *notifier.Base
	cfg  Config
	tmpl *template.Template
}

// New creates a new Slack notifier with the given configuration.
func New(cfg Config, log *zap.Logger) (*Notifier, error) {
	if cfg.Username == "" {
		cfg.Username = "tekton-events-relay"
	}
	if cfg.IconEmoji == "" {
		cfg.IconEmoji = ":rocket:"
	}

	n := &Notifier{cfg: cfg}

	// Compile template if provided (TemplateFile takes precedence)
	var templateContent string
	if cfg.TemplateFile != "" {
		data, err := os.ReadFile(cfg.TemplateFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read template file %s: %w", cfg.TemplateFile, err)
		}
		templateContent = string(data)
	} else if cfg.Template != "" {
		templateContent = cfg.Template
	}

	if templateContent != "" {
		tmpl, err := template.New("slack").Parse(templateContent)
		if err != nil {
			return nil, fmt.Errorf("invalid template: %w", err)
		}
		n.tmpl = tmpl
	}

	base := &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildPayload: n.payload,
		UserAgent:    notifier.UserAgent,
		Log:          log,
	}

	if cfg.BotToken != "" {
		// Bot token mode: post to chat.postMessage API
		base.BuildURL = func(_ domain.Event) (string, error) {
			return "https://slack.com/api/chat.postMessage", nil
		}
		token := cfg.BotToken
		base.Auth = func(req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}
		// In bot token mode, channel must be set via ChannelID if Channel not provided
		if cfg.Channel == "" && cfg.ChannelID != "" {
			cfg.Channel = cfg.ChannelID
		}
	} else {
		base.BuildURL = func(_ domain.Event) (string, error) { return cfg.WebhookURL, nil }
	}

	n.cfg = cfg
	n.base = base
	return n, nil
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "slack" }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle sends the event to Slack.
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

		msg := map[string]any{
			"username":   n.cfg.Username,
			"icon_emoji": n.cfg.IconEmoji,
			"text":       buf.String(),
		}
		if n.cfg.Channel != "" {
			msg["channel"] = n.cfg.Channel
		}
		return msg, nil
	}

	// Use default format
	color := colorFor(e.State)
	emoji := emojiFor(e.State)

	text := fmt.Sprintf("%s *%s* — %s", emoji, e.Context, e.Description)
	if e.TargetURL != "" {
		text += fmt.Sprintf("\n<%s|View run>", e.TargetURL)
	}

	msg := map[string]any{
		"username":   n.cfg.Username,
		"icon_emoji": n.cfg.IconEmoji,
		"attachments": []map[string]any{
			{
				"color":     color,
				"text":      text,
				"footer":    fmt.Sprintf("%s/%s", e.Namespace, e.RunName),
				"mrkdwn_in": []string{"text"},
				"fields":    fields(e),
			},
		},
	}
	if n.cfg.Channel != "" {
		msg["channel"] = n.cfg.Channel
	}
	return msg, nil
}

func fields(e domain.Event) []map[string]any {
	f := []map[string]any{
		{fieldKeyTitle: "State", fieldKeyValue: strings.ToUpper(string(e.State)), fieldKeyShort: true},
		{fieldKeyTitle: "Run", fieldKeyValue: e.RunName, fieldKeyShort: true},
	}
	if e.CommitSHA != "" {
		f = append(f, map[string]any{
			"title": fieldTitleCommit, fieldKeyValue: e.CommitSHA[:min(7, len(e.CommitSHA))], fieldKeyShort: true,
		})
	}
	if !e.StartedAt.IsZero() && !e.FinishedAt.IsZero() {
		d := e.FinishedAt.Sub(e.StartedAt).Round(1e9)
		f = append(f, map[string]any{"title": fieldTitleDuration, fieldKeyValue: d.String(), fieldKeyShort: true})
	}
	return f
}

func colorFor(s domain.State) string {
	switch s {
	case domain.StateSuccess:
		return "#36a64f"
	case domain.StateFailure, domain.StateError:
		return colorFailure
	case domain.StateRunning:
		return "#daa038"
	default:
		return colorUnknown
	}
}

func emojiFor(s domain.State) string {
	switch s {
	case domain.StateSuccess:
		return ":white_check_mark:"
	case domain.StateFailure, domain.StateError:
		return emojiFailure
	case domain.StateRunning:
		return ":hourglass_flowing_sand:"
	default:
		return emojiUnknown
	}
}
