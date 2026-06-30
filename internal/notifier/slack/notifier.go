// Package slack implements the Notifier for Slack.
//
// Two transports are supported:
//   - Incoming Webhook (cfg.WebhookURL) — fire-and-forget, no message identity,
//     so upsert is NOT available (the webhook returns no message "ts").
//   - Bot token (cfg.Token, a scm.TokenRefresher) via the slack-go SDK —
//     supports chat.postMessage / chat.update (upsert keyed by stored ts) and
//     optional thread replies (thread_ts).
//
// State filtering is performed via CEL expressions in the `when` field of the action config,
// evaluated by the middleware layer (internal/notifier/middleware/cel.go).
// If no `when` expression is set, the handler processes all events.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	slackgo "github.com/slack-go/slack"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/msgstore"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
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
	payloadKeyText     = "text"

	// ModeCreate always posts a new message (default).
	ModeCreate = "create"
	// ModeUpsert edits the original message for a RunID (chat.update), keyed
	// by the ts captured on the first post. Bot token transport only.
	ModeUpsert = "upsert"

	// ThreadModeGrouped posts the first event of a RunID as a top-level
	// message and replies in a thread for subsequent events. Mutually
	// exclusive with ModeUpsert. Bot token transport only.
	ThreadModeGrouped = "grouped"

	botHTTPTimeout = 10 * time.Second
)

// Config holds the Slack notifier configuration.
type Config struct {
	// Webhook mode
	WebhookURL string
	// Bot token mode: a per-request token source (never a static string), so
	// rotated Kubernetes secrets / OAuth2 tokens are picked up without restart.
	Token     scm.TokenRefresher
	ChannelID string
	// Common
	Channel     string             // optional — override of the channel configured in webhook
	ChannelExpr *cel.StringProgram // optional CEL expression for dynamic channel routing
	Username    string             // displayed name; default: tekton-events-relay
	IconEmoji   string             // default: :rocket:
	Template    string             // optional Go template; if empty, uses default format

	// Mode is "create" (default) or "upsert". Upsert requires bot token mode
	// and a MessageStore; it edits the original message per RunID.
	Mode string
	// ThreadMode is "grouped" (or empty). Grouped posts the first event per
	// RunID as a top-level message and replies in a thread for subsequent
	// events. Mutually exclusive with Mode "upsert". Requires MessageStore.
	ThreadMode string
	// ThreadTS, when set, posts/updates the message as a reply in that thread.
	ThreadTS string
	// MessageStore persists the message ts per RunID for upsert mode. When nil,
	// upsert degrades to posting a new message (fail-open).
	MessageStore msgstore.Store

	// HTTPClient overrides the HTTP client for webhook mode. When nil,
	// notifier.DefaultHTTPClient() is used.
	HTTPClient *http.Client
	// RetryPolicy overrides the global retry policy. When nil, the global
	// default is used.
	RetryPolicy *httpx.RetryPolicy

	// apiURL overrides the Slack API base URL. Test-only.
	apiURL string
}

// Notifier implements the notifier for Slack.
type Notifier struct {
	base *notifier.Base  // webhook transport
	api  *slackgo.Client // bot token transport (nil in webhook mode)
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
	if cfg.Mode == "" {
		cfg.Mode = ModeCreate
	}

	n := &Notifier{cfg: cfg}

	// Resolve the template: inline string or an /etc/templates/... path
	// (the chart renders configmap defaults / configmapRef as a path).
	templateContent, err := scm.LoadTemplateString(cfg.Template)
	if err != nil {
		return nil, fmt.Errorf("load template: %w", err)
	}
	if templateContent != "" {
		tmpl, err := template.New("slack").Parse(templateContent)
		if err != nil {
			return nil, fmt.Errorf("invalid template: %w", err)
		}
		n.tmpl = tmpl
	}

	if cfg.Token != nil {
		// Bot token mode: drive the slack-go SDK with a per-request token.
		// The TokenTransport overrides Authorization on every request, so the
		// empty static token handed to slack.New is never used.
		httpClient := &http.Client{
			Timeout: botHTTPTimeout,
			Transport: &scm.TokenTransport{
				Base:      http.DefaultTransport,
				Refresher: cfg.Token,
				Style:     scm.AuthStyleBearer,
			},
		}
		opts := []slackgo.Option{slackgo.OptionHTTPClient(httpClient)}
		if cfg.apiURL != "" {
			opts = append(opts, slackgo.OptionAPIURL(cfg.apiURL))
		}
		n.api = slackgo.New("", opts...)
		if n.cfg.Channel == "" && cfg.ChannelID != "" {
			n.cfg.Channel = cfg.ChannelID
		}
		return n, nil
	}

	// Webhook mode (no upsert support — webhooks return no message ts).
	httpClient := notifier.DefaultHTTPClient()
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}
	n.base = &notifier.Base{
		HTTP:         httpClient,
		BuildPayload: n.payload,
		BuildURL:     func(_ domain.Event) (string, error) { return cfg.WebhookURL, nil },
		UserAgent:    notifier.UserAgent,
		Log:          log,
		RetryPolicy:  cfg.RetryPolicy,
	}
	return n, nil
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "slack" }

// Provider returns the provider type identifier.
func (n *Notifier) Provider() string { return "slack" }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Close releases resources held by the handler.
func (n *Notifier) Close() error { return nil }

// Flush sends multiple events as a single combined Slack message.
// For webhook mode, all events are merged into one payload with multiple
// attachments. For bot token mode, all events become attachments in one
// PostMessage call.
func (n *Notifier) Flush(ctx context.Context, events []domain.Event) error {
	if len(events) == 0 {
		return nil
	}
	if n.api != nil {
		return n.flushBot(ctx, events)
	}
	return n.flushWebhook(ctx, events)
}

func (n *Notifier) flushBot(ctx context.Context, events []domain.Event) error {
	attachments := make([]slackgo.Attachment, 0, len(events))
	for _, e := range events {
		attachments = append(attachments, n.attachment(e))
	}

	opts := []slackgo.MsgOption{
		slackgo.MsgOptionAttachments(attachments...),
	}
	if n.cfg.Username != "" {
		opts = append(opts, slackgo.MsgOptionUsername(n.cfg.Username))
	}
	if n.cfg.IconEmoji != "" {
		opts = append(opts, slackgo.MsgOptionIconEmoji(n.cfg.IconEmoji))
	}

	channel := n.resolveChannel(events[0])
	if _, _, err := n.api.PostMessageContext(ctx, channel, opts...); err != nil {
		return fmt.Errorf("slack batch post: %w", err)
	}
	return nil
}

func (n *Notifier) flushWebhook(ctx context.Context, events []domain.Event) error {
	attachments := make([]map[string]any, 0, len(events))
	for _, e := range events {
		attachments = append(attachments, map[string]any{
			"color":        colorFor(e.State),
			payloadKeyText: defaultText(e),
			"footer":       fmt.Sprintf("%s/%s", e.Namespace, e.RunName),
			"mrkdwn_in":    []string{"text"},
			"fields":       fields(e),
		})
	}

	msg := map[string]any{
		"username":    n.cfg.Username,
		"icon_emoji":  n.cfg.IconEmoji,
		"attachments": attachments,
	}
	if ch := n.resolveChannel(events[0]); ch != "" {
		msg["channel"] = ch
	}

	url, err := n.base.BuildURL(events[0])
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal batch payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
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
		return fmt.Errorf("slack batch webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("slack batch webhook responded %d: %s", resp.StatusCode, string(buf))
	}
	return nil
}

// Handle sends the event to Slack via the configured transport.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	if n.api != nil {
		return n.sendBot(ctx, e)
	}
	return n.base.Send(ctx, e)
}

// resolveChannel determines the target channel: CEL expression (dynamic),
// static Channel field, or ChannelID (bot mode fallback).
func (n *Notifier) resolveChannel(e domain.Event) string {
	if n.cfg.ChannelExpr != nil {
		if ch, err := n.cfg.ChannelExpr.EvalString(e); err == nil && ch != "" {
			return ch
		}
	}
	if n.cfg.Channel != "" {
		return n.cfg.Channel
	}
	return n.cfg.ChannelID
}

// sendBot posts (or, in upsert mode, edits) a message via the bot token API.
func (n *Notifier) sendBot(ctx context.Context, e domain.Event) error {
	opts, err := n.messageOptions(e)
	if err != nil {
		return err
	}
	channel := n.resolveChannel(e)

	// Thread grouped: first event posts top-level, subsequent events reply
	// in the thread. Runs before upsert — the two modes are mutually exclusive.
	if n.canThreadGroup() {
		if ts, ok := n.cfg.MessageStore.Load(e.RunID); ok && ts != "" {
			opts = append(opts, slackgo.MsgOptionTS(ts))
		}
		_, ts, perr := n.api.PostMessageContext(ctx, channel, opts...)
		if perr != nil {
			return fmt.Errorf("slack post message: %w", perr)
		}
		if e.RunID != "" && ts != "" {
			n.cfg.MessageStore.Save(e.RunID, ts)
		}
		return nil
	}

	// Upsert: edit the original message if we have a stored ts for this run.
	if n.canUpsert() {
		if ts, ok := n.cfg.MessageStore.Load(e.RunID); ok && ts != "" {
			if _, _, _, uerr := n.api.UpdateMessageContext(ctx, channel, ts, opts...); uerr != nil {
				return fmt.Errorf("slack update message: %w", uerr)
			}
			return nil
		}
	}

	_, ts, err := n.api.PostMessageContext(ctx, channel, opts...)
	if err != nil {
		return fmt.Errorf("slack post message: %w", err)
	}
	if n.canUpsert() && e.RunID != "" && ts != "" {
		n.cfg.MessageStore.Save(e.RunID, ts)
	}
	return nil
}

// canUpsert reports whether upsert is active and backed by a store.
func (n *Notifier) canUpsert() bool {
	return n.cfg.Mode == ModeUpsert && n.cfg.MessageStore != nil
}

// canThreadGroup reports whether thread-grouped mode is active and backed by a store.
func (n *Notifier) canThreadGroup() bool {
	return n.cfg.ThreadMode == ThreadModeGrouped && n.cfg.MessageStore != nil
}

// messageOptions builds the slack-go MsgOptions for the bot token transport.
func (n *Notifier) messageOptions(e domain.Event) ([]slackgo.MsgOption, error) {
	var opts []slackgo.MsgOption
	if n.tmpl != nil {
		var buf bytes.Buffer
		if err := n.tmpl.Execute(&buf, e); err != nil {
			return nil, fmt.Errorf("template execution failed: %w", err)
		}
		opts = append(opts, slackgo.MsgOptionText(buf.String(), false))
	} else {
		opts = append(opts, slackgo.MsgOptionAttachments(n.attachment(e)))
	}
	if n.cfg.Username != "" {
		opts = append(opts, slackgo.MsgOptionUsername(n.cfg.Username))
	}
	if n.cfg.IconEmoji != "" {
		opts = append(opts, slackgo.MsgOptionIconEmoji(n.cfg.IconEmoji))
	}
	if n.cfg.ThreadTS != "" {
		opts = append(opts, slackgo.MsgOptionTS(n.cfg.ThreadTS))
	}
	return opts, nil
}

// attachment builds the default-format Slack attachment for bot token mode.
func (n *Notifier) attachment(e domain.Event) slackgo.Attachment {
	return slackgo.Attachment{
		Color:      colorFor(e.State),
		Text:       defaultText(e),
		Footer:     fmt.Sprintf("%s/%s", e.Namespace, e.RunName),
		MarkdownIn: []string{"text"},
		Fields:     attachmentFields(e),
	}
}

func (n *Notifier) payload(e domain.Event) (any, error) {
	// Use custom template if available
	if n.tmpl != nil {
		var buf bytes.Buffer
		if err := n.tmpl.Execute(&buf, e); err != nil {
			return nil, fmt.Errorf("template execution failed: %w", err)
		}

		msg := map[string]any{
			"username":     n.cfg.Username,
			"icon_emoji":   n.cfg.IconEmoji,
			payloadKeyText: buf.String(),
		}
		if ch := n.resolveChannel(e); ch != "" {
			msg["channel"] = ch
		}
		return msg, nil
	}

	// Use default format
	msg := map[string]any{
		"username":   n.cfg.Username,
		"icon_emoji": n.cfg.IconEmoji,
		"attachments": []map[string]any{
			{
				"color":        colorFor(e.State),
				payloadKeyText: defaultText(e),
				"footer":       fmt.Sprintf("%s/%s", e.Namespace, e.RunName),
				"mrkdwn_in":    []string{"text"},
				"fields":       fields(e),
			},
		},
	}
	if ch := n.resolveChannel(e); ch != "" {
		msg["channel"] = ch
	}
	return msg, nil
}

// defaultText renders the default message body shared by both transports.
func defaultText(e domain.Event) string {
	text := fmt.Sprintf("%s *%s* — %s", emojiFor(e.State), e.Context, e.Description)
	if e.TargetURL != "" {
		text += fmt.Sprintf("\n<%s|View run>", e.TargetURL)
	}
	return text
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

// attachmentFields mirrors fields() as slack-go AttachmentFields for bot mode.
func attachmentFields(e domain.Event) []slackgo.AttachmentField {
	f := []slackgo.AttachmentField{
		{Title: "State", Value: strings.ToUpper(string(e.State)), Short: true},
		{Title: "Run", Value: e.RunName, Short: true},
	}
	if e.CommitSHA != "" {
		f = append(f, slackgo.AttachmentField{
			Title: fieldTitleCommit, Value: e.CommitSHA[:min(7, len(e.CommitSHA))], Short: true,
		})
	}
	if !e.StartedAt.IsZero() && !e.FinishedAt.IsZero() {
		d := e.FinishedAt.Sub(e.StartedAt).Round(1e9)
		f = append(f, slackgo.AttachmentField{Title: fieldTitleDuration, Value: d.String(), Short: true})
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
