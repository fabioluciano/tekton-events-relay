// Package discord implements the Notifier for Discord via Webhook.
// Doc: https://discord.com/developers/docs/resources/webhook#execute-webhook
package discord

import (
	"go.uber.org/zap"

	"bytes"
	"context"
	"fmt"
	"net/http"
	"text/template"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	defaultUsername = "tekton-events-relay"
	fieldInline     = "inline"
	fieldCommit     = "Commit"
	fieldName       = "name"
	fieldValue      = "value"
)

// Config holds the configuration for the Discord notifier.
type Config struct {
	// Webhook mode
	WebhookURL string
	// Bot token mode
	BotToken  string
	ChannelID string // Discord channel snowflake ID
	// Common
	Username string // default: tekton-events-relay
	Template string // optional Go template; if empty, uses default format
}

// Notifier sends events to Discord via webhook.
type Notifier struct {
	base *notifier.Base
	cfg  Config
	tmpl *template.Template
}

// New creates a new Discord notifier with the given configuration.
func New(cfg Config, log *zap.Logger) (*Notifier, error) {
	if cfg.Username == "" {
		cfg.Username = defaultUsername
	}

	n := &Notifier{cfg: cfg}

	// Resolve the template: inline string or an /etc/templates/... path
	// (the chart renders configmap defaults / configmapRef as a path).
	templateContent, err := scm.LoadTemplateString(cfg.Template)
	if err != nil {
		return nil, fmt.Errorf("load template: %w", err)
	}

	if templateContent != "" {
		tmpl, err := template.New("discord").Parse(templateContent)
		if err != nil {
			return nil, fmt.Errorf("invalid template: %w", err)
		}
		n.tmpl = tmpl
	}

	base := &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildPayload: n.payload,
		UserAgent:    defaultUsername,
		Log:          log,
	}

	if cfg.BotToken != "" {
		// Bot token mode: post to channels/{id}/messages API
		channelID := cfg.ChannelID
		base.BuildURL = func(_ domain.Event) (string, error) {
			return fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID), nil
		}
		token := cfg.BotToken
		base.Auth = func(req *http.Request) error {
			req.Header.Set("Authorization", "Bot "+token)
			return nil
		}
	} else {
		base.BuildURL = func(_ domain.Event) (string, error) { return cfg.WebhookURL, nil }
	}

	n.base = base
	return n, nil
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "discord" }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle sends the event to Discord.
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
			"username": n.cfg.Username,
			"content":  buf.String(),
		}, nil
	}

	// Use default format
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
