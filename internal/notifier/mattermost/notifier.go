// Package mattermost implements the Notifier for Mattermost.
//
// Two transports are supported:
//   - Incoming Webhook (cfg.WebhookURL) — fire-and-forget, posts to a
//     pre-configured webhook URL with {"text":"...","username":"...","icon_url":"..."}.
//   - Bot token (cfg.Token, a scm.TokenRefresher) — posts via the Mattermost
//     REST API at {base_url}/api/v4/posts with Authorization: Bearer {token}
//     and requires cfg.ChannelID.
//
// State filtering is performed via CEL expressions in the `when` field of the action config,
// evaluated by the middleware layer (internal/notifier/middleware/cel.go).
// If no `when` expression is set, the handler processes all events.
package mattermost

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"text/template"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	colorFailure = "#e01e5a"
	colorUnknown = "#aaaaaa"
	emojiFailure = ":x:"
	emojiUnknown = ":grey_question:"
)

// Config holds the Mattermost notifier configuration.
type Config struct {
	// Webhook mode
	WebhookURL string
	// Bot token mode: a per-request token source (never a static string), so
	// rotated Kubernetes secrets are picked up without restart.
	Token     scm.TokenRefresher
	BaseURL   string // Mattermost server URL (e.g. "https://mattermost.example.com")
	ChannelID string // required for bot token mode
	// Common
	Channel  string // optional — override of the channel configured in webhook
	Username string // displayed name; default: tekton-events-relay
	IconURL  string // optional icon URL
	Template string // optional Go template; if empty, uses default format

	// HTTPClient overrides the HTTP client for webhook mode. When nil,
	// notifier.DefaultHTTPClient() is used.
	HTTPClient *http.Client
	// RetryPolicy overrides the global retry policy. When nil, the global
	// default is used.
	RetryPolicy *httpx.RetryPolicy
}

// Notifier implements the notifier for Mattermost.
type Notifier struct {
	base *notifier.Base // webhook transport
	cfg  Config
	tmpl *template.Template
	// botMode indicates bot token mode is active.
	botMode bool
}

// New creates a new Mattermost notifier with the given configuration.
func New(cfg Config, log *zap.Logger) (*Notifier, error) {
	if cfg.Username == "" {
		cfg.Username = "tekton-events-relay"
	}

	n := &Notifier{cfg: cfg}

	// Resolve the template: inline string or an /etc/templates/... path
	templateContent, err := scm.LoadTemplateString(cfg.Template)
	if err != nil {
		return nil, fmt.Errorf("load template: %w", err)
	}
	if templateContent != "" {
		tmpl, err := template.New("mattermost").Parse(templateContent)
		if err != nil {
			return nil, fmt.Errorf("invalid template: %w", err)
		}
		n.tmpl = tmpl
	}

	if cfg.Token != nil {
		// Bot token mode: use notifier.Base with Bearer auth.
		n.botMode = true
		httpClient := notifier.DefaultHTTPClient()
		if cfg.HTTPClient != nil {
			httpClient = cfg.HTTPClient
		}
		n.base = &notifier.Base{
			HTTP:         httpClient,
			BuildPayload: n.botPayload,
			BuildURL:     n.botURL,
			Auth:         n.botAuth,
			UserAgent:    notifier.UserAgent,
			Log:          log,
			RetryPolicy:  cfg.RetryPolicy,
		}
		return n, nil
	}

	// Webhook mode
	httpClient := notifier.DefaultHTTPClient()
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}
	n.base = &notifier.Base{
		HTTP:         httpClient,
		BuildPayload: n.webhookPayload,
		BuildURL:     func(_ domain.Event) (string, error) { return cfg.WebhookURL, nil },
		UserAgent:    notifier.UserAgent,
		Log:          log,
		RetryPolicy:  cfg.RetryPolicy,
	}
	return n, nil
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "mattermost" }

// Provider returns the provider type identifier.
func (n *Notifier) Provider() string { return "mattermost" }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Close releases resources held by the handler.
func (n *Notifier) Close() error { return nil }

// Handle sends the event to Mattermost via the configured transport.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	return n.base.Send(ctx, e)
}

// botURL constructs the Mattermost REST API URL for creating a post.
func (n *Notifier) botURL(_ domain.Event) (string, error) {
	return fmt.Sprintf("%s/api/v4/posts", strings.TrimRight(n.cfg.BaseURL, "/")), nil
}

// botAuth applies Bearer token authentication for bot token mode.
func (n *Notifier) botAuth(req *http.Request) error {
	tok, err := n.cfg.Token.Token(req.Context())
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return nil
}

// botPayload builds the Mattermost API payload for bot token mode.
func (n *Notifier) botPayload(e domain.Event) (any, error) {
	channelID := n.cfg.ChannelID
	if n.cfg.Channel != "" {
		channelID = n.cfg.Channel
	}

	text, err := n.renderText(e)
	if err != nil {
		return nil, err
	}

	msg := map[string]any{
		"channel_id": channelID,
		"message":    text,
	}

	if n.cfg.IconURL != "" {
		msg["props"] = map[string]any{
			"from_webhook":      "true",
			"override_icon_url": n.cfg.IconURL,
			"override_username": n.cfg.Username,
		}
	} else {
		msg["props"] = map[string]any{
			"from_webhook":      "true",
			"override_username": n.cfg.Username,
		}
	}

	return msg, nil
}

// webhookPayload builds the Mattermost webhook payload.
func (n *Notifier) webhookPayload(e domain.Event) (any, error) {
	text, err := n.renderText(e)
	if err != nil {
		return nil, err
	}

	msg := map[string]any{
		"text":     text,
		"username": n.cfg.Username,
	}
	if n.cfg.IconURL != "" {
		msg["icon_url"] = n.cfg.IconURL
	}
	if n.cfg.Channel != "" {
		// Mattermost webhooks support channel override
		msg["channel"] = n.cfg.Channel
	}
	return msg, nil
}

// renderText renders the message text from the template or default format.
func (n *Notifier) renderText(e domain.Event) (string, error) {
	if n.tmpl != nil {
		var buf bytes.Buffer
		if err := n.tmpl.Execute(&buf, e); err != nil {
			return "", fmt.Errorf("template execution failed: %w", err)
		}
		return buf.String(), nil
	}
	return defaultText(e), nil
}

// defaultText renders the default message body.
func defaultText(e domain.Event) string {
	text := fmt.Sprintf("%s **%s** — %s", emojiFor(e.State), e.Context, e.Description)
	if e.TargetURL != "" {
		text += fmt.Sprintf("\n[View run](%s)", e.TargetURL)
	}
	return text
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
