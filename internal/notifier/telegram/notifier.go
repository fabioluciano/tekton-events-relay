// Package telegram implements the Notifier for Telegram via the Bot API.
// Sends messages to a chat or channel using the sendMessage endpoint with
// MarkdownV2 parse mode.
// Doc: https://core.telegram.org/bots/api#sendmessage
package telegram

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

// Compile-time checks.
var _ notifier.ActionHandler = (*Notifier)(nil)

const (
	notifierName   = "telegram"
	apiURLTemplate = "https://api.telegram.org/bot%s/sendMessage"

	// defaultTemplate is a Category-2 fallback: when the user provides no
	// template, the chart renders this path via configmap-templates.yaml and
	// scm.LoadTemplateString loads the file. We never hardcode the content
	// here — the const is the default Go template string used only when no
	// chart template is mounted AND no inline template is provided.
	defaultTemplateContent = "Pipeline {{.RunName}} - {{.State}}"
)

// markdownV2Special lists every character Telegram MarkdownV2 requires escaping
// outside of code/pre blocks. The backslash-escape must happen BEFORE template
// execution so literal underscores in RunName don't break parsing.
var markdownV2Special = strings.NewReplacer(
	"_", "\\_",
	"*", "\\*",
	"[", "\\[",
	"]", "\\]",
	"(", "\\(",
	")", "\\)",
	"~", "\\~",
	"`", "\\`",
	">", "\\>",
	"#", "\\#",
	"+", "\\+",
	"-", "\\-",
	"=", "\\=",
	"|", "\\|",
	"{", "\\{",
	"}", "\\}",
	".", "\\.",
	"!", "\\!",
)

// Config holds the Telegram notifier configuration.
type Config struct {
	Name string
	// Token provides the bot API token, resolved fresh per request so rotated
	// secrets are picked up without a pod restart.
	Token scm.TokenRefresher
	// ChatID is the target chat or channel (numeric ID or @channel_username).
	ChatID string
	// Template is an optional Go template for the message body. When empty, a
	// default template is used (Category 2: optional with native fallback).
	Template string
	// HTTPClient overrides the HTTP client. When nil, notifier.DefaultHTTPClient() is used.
	HTTPClient *http.Client
	// RetryPolicy overrides the global retry policy. When nil, the global default is used.
	RetryPolicy *httpx.RetryPolicy
}

// Notifier sends messages to Telegram via the Bot API.
type Notifier struct {
	base   *notifier.Base
	name   string
	chatID string
	tmpl   *template.Template
}

// New creates a new Telegram notifier with the given configuration.
func New(cfg Config, log *zap.Logger) (*Notifier, error) {
	if cfg.Token == nil {
		return nil, fmt.Errorf("telegram: token refresher is required")
	}
	if cfg.ChatID == "" {
		return nil, fmt.Errorf("telegram: chat_id is required")
	}

	// Category 2: resolve template content. If the user provides a template
	// (inline or file path), use it. Otherwise fall back to the default.
	templateContent := cfg.Template
	if templateContent == "" {
		templateContent = defaultTemplateContent
	}

	tmpl, err := scm.CompileTemplate("telegram", templateContent, nil)
	if err != nil {
		return nil, fmt.Errorf("compile template: %w", err)
	}

	n := &Notifier{
		name:   cfg.Name,
		chatID: cfg.ChatID,
		tmpl:   tmpl,
	}

	token := cfg.Token
	httpClient := notifier.DefaultHTTPClient()
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}

	n.base = &notifier.Base{
		HTTP:        httpClient,
		UserAgent:   notifier.UserAgent,
		Log:         log,
		RetryPolicy: cfg.RetryPolicy,
		BuildURL: func(_ domain.Event) (string, error) {
			tok, err := token.Token(context.Background())
			if err != nil {
				return "", fmt.Errorf("telegram: resolve token: %w", err)
			}
			return fmt.Sprintf(apiURLTemplate, tok), nil
		},
		BuildPayload: n.payload,
	}
	return n, nil
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return n.name }

// Provider returns the provider type identifier.
func (n *Notifier) Provider() string { return notifierName }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle sends the event to Telegram.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	return n.base.Send(ctx, e)
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (n *Notifier) Close() error { return nil }

// payload builds the Telegram sendMessage payload from the domain.Event.
func (n *Notifier) payload(e domain.Event) (any, error) {
	var buf bytes.Buffer
	if err := n.tmpl.Execute(&buf, e); err != nil {
		return nil, fmt.Errorf("template execution failed: %w", err)
	}

	// Escape MarkdownV2 special characters in the rendered text.
	text := EscapeMarkdownV2(buf.String())

	return map[string]any{
		"chat_id":    n.chatID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	}, nil
}

// EscapeMarkdownV2 escapes all special characters required by Telegram's
// MarkdownV2 parse mode. This must be applied to dynamic content after
// template execution.
func EscapeMarkdownV2(s string) string {
	return markdownV2Special.Replace(s)
}
