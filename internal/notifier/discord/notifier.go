// Package discord implements the Notifier for Discord via the discordgo SDK.
//
// Two transports are supported, both using only Discord REST endpoints — the
// gateway WebSocket is never opened:
//   - Incoming Webhook (cfg.WebhookURL) — WebhookExecute(wait=true) captures the
//     created message ID; mode=upsert edits the original message per RunID via
//     WebhookMessageEdit (falling back to a new post when no ID is stored).
//   - Bot token (cfg.BotToken, a scm.TokenRefresher) — channel message
//     send/edit; the token is resolved per request (rotation-safe), never a
//     static string baked in at build time.
//
// State filtering is performed via CEL expressions in the `when` field of the
// action config, evaluated by the middleware layer; if no `when` is set, the
// handler processes all events.
package discord

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/msgstore"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	defaultUsername = "tekton-events-relay"
	fieldCommit     = "Commit"

	// ModeCreate always posts a new message (default).
	ModeCreate = "create"
	// ModeUpsert edits the original message for a RunID, keyed by the message
	// ID captured on the first post. Degrades to a new post when no ID is
	// stored (fail-open).
	ModeUpsert = "upsert"

	// ThreadModeGrouped posts the first event of a RunID as a top-level
	// message and replies in a thread for subsequent events. Mutually
	// exclusive with ModeUpsert.
	ThreadModeGrouped = "grouped"

	httpTimeout = 10 * time.Second
)

// Config holds the configuration for the Discord notifier.
type Config struct {
	// Webhook mode: the full Discord webhook URL
	// (https://discord.com/api/webhooks/{id}/{token}).
	WebhookURL string
	// Bot token mode: a per-request token source (never a static string), so
	// rotated Kubernetes secrets / OAuth2 tokens are picked up without restart.
	BotToken  scm.TokenRefresher
	ChannelID string // Discord channel snowflake ID
	// Common
	Username     string   // displayed name (webhook only); default: tekton-events-relay
	Template     string   // optional Go template; if empty, uses the default embed
	MentionRoles []string // Discord role IDs to mention in the message content

	// Mode is "create" (default) or "upsert". Upsert edits the original message
	// per RunID; requires a MessageStore (degrades to create otherwise).
	Mode string
	// ThreadMode is "grouped" (or empty). Grouped posts the first event per
	// RunID as a top-level message and replies in a thread for subsequent
	// events. Mutually exclusive with Mode "upsert". Requires MessageStore.
	ThreadMode string
	// MessageStore persists the message ID per RunID for upsert/thread mode.
	// When nil, upsert/thread degrades to posting a new message (fail-open).
	MessageStore msgstore.Store

	// httpClient overrides the base RoundTripper of the discordgo session.
	// Test-only seam for redirecting bot-mode requests at a stub server.
	httpClient *http.Client
}

// Notifier sends events to Discord via the discordgo SDK.
type Notifier struct {
	cfg          Config
	tmpl         *template.Template
	log          *zap.Logger
	session      *discordgo.Session
	webhookID    string
	webhookToken string
	botMode      bool
}

// botTokenTransport injects "Authorization: Bot {token}" on every request,
// resolving a fresh token each time so rotated secrets / OAuth2 tokens take
// effect without recreating the SDK client.
type botTokenTransport struct {
	base      http.RoundTripper
	refresher scm.TokenRefresher
}

func (t *botTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.refresher.Token(req.Context())
	if err != nil {
		return nil, fmt.Errorf("refresh discord bot token: %w", err)
	}
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bot "+tok)
	return t.base.RoundTrip(req)
}

// hostRewriteTransport pins every request to a fixed scheme+host. In production
// this is the configured webhook host (identity for discord.com), so it also
// transparently supports Discord-compatible proxies; in tests it redirects the
// SDK's discord.com requests at a stub server.
type hostRewriteTransport struct {
	base   http.RoundTripper
	scheme string
	host   string
}

func (t *hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = t.scheme
	req.URL.Host = t.host
	req.Host = t.host
	return t.base.RoundTrip(req)
}

// New creates a new Discord notifier with the given configuration.
func New(cfg Config, log *zap.Logger) (*Notifier, error) {
	if cfg.Username == "" {
		cfg.Username = defaultUsername
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeCreate
	}
	if log == nil {
		log = zap.NewNop()
	}

	n := &Notifier{cfg: cfg, log: log}

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

	// discordgo.New("") builds a REST-only session; the gateway is never opened.
	session, err := discordgo.New("")
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	base := http.DefaultTransport
	if cfg.httpClient != nil && cfg.httpClient.Transport != nil {
		base = cfg.httpClient.Transport
	}

	if cfg.BotToken != nil {
		n.botMode = true
		session.Client = &http.Client{
			Timeout:   httpTimeout,
			Transport: &botTokenTransport{base: base, refresher: cfg.BotToken},
		}
	} else {
		id, token, perr := parseWebhookURL(cfg.WebhookURL)
		if perr != nil {
			return nil, perr
		}
		u, perr := url.Parse(cfg.WebhookURL)
		if perr != nil {
			return nil, fmt.Errorf("parse discord webhook URL: %w", perr)
		}
		n.webhookID = id
		n.webhookToken = token
		session.Client = &http.Client{
			Timeout:   httpTimeout,
			Transport: &hostRewriteTransport{base: base, scheme: u.Scheme, host: u.Host},
		}
	}

	n.session = session
	return n, nil
}

// parseWebhookURL extracts the webhook ID and token from a Discord webhook URL
// of the form .../webhooks/{id}/{token}.
func parseWebhookURL(raw string) (id, token string, err error) {
	if raw == "" {
		return "", "", fmt.Errorf("discord webhook URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse discord webhook URL: %w", err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "webhooks" && parts[i+1] != "" && parts[i+2] != "" {
			return parts[i+1], parts[i+2], nil
		}
	}
	return "", "", fmt.Errorf("invalid discord webhook URL: %q (expected .../webhooks/{id}/{token})", raw)
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "discord" }

// Provider returns the provider type identifier.
func (n *Notifier) Provider() string { return "discord" }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Close releases resources held by the notifier. Idempotent.
func (n *Notifier) Close() error { return nil }

// Flush sends multiple events as a single Discord message with multiple embeds.
// Each event becomes one embed in the message.
func (n *Notifier) Flush(ctx context.Context, events []domain.Event) error {
	if len(events) == 0 {
		return nil
	}

	embeds := make([]*discordgo.MessageEmbed, 0, len(events))
	for _, e := range events {
		embeds = append(embeds, n.embed(e))
	}

	if n.botMode {
		_, err := n.session.ChannelMessageSendComplex(n.cfg.ChannelID, &discordgo.MessageSend{
			Embeds: embeds,
		}, discordgo.WithContext(ctx))
		if err != nil {
			return fmt.Errorf("discord batch send: %w", err)
		}
		return nil
	}

	_, err := n.session.WebhookExecute(n.webhookID, n.webhookToken, true, &discordgo.WebhookParams{
		Username: n.cfg.Username,
		Embeds:   embeds,
	}, discordgo.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("discord batch webhook: %w", err)
	}
	return nil
}

// Handle sends the event to Discord via the configured transport.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	if n.botMode {
		return n.sendBot(ctx, e)
	}
	return n.sendWebhook(ctx, e)
}

// canUpsert reports whether upsert is active and backed by a store.
func (n *Notifier) canUpsert() bool {
	return n.cfg.Mode == ModeUpsert && n.cfg.MessageStore != nil
}

// canThreadGroup reports whether thread-grouped mode is active and backed by a store.
func (n *Notifier) canThreadGroup() bool {
	return n.cfg.ThreadMode == ThreadModeGrouped && n.cfg.MessageStore != nil
}

// sendWebhook posts (or, in upsert mode, edits) a message via the webhook API.
func (n *Notifier) sendWebhook(ctx context.Context, e domain.Event) error {
	content, embeds, err := n.render(e)
	if err != nil {
		return err
	}

	// Upsert: edit the original message if we have a stored ID for this run.
	if n.canUpsert() {
		if id, ok := n.cfg.MessageStore.Load(e.RunID); ok && id != "" {
			edit := &discordgo.WebhookEdit{}
			if content != "" {
				edit.Content = &content
			}
			if len(embeds) > 0 {
				edit.Embeds = &embeds
			}
			if _, eerr := n.session.WebhookMessageEdit(n.webhookID, n.webhookToken, id, edit, discordgo.WithContext(ctx)); eerr == nil {
				n.log.Debug("discord webhook message edited", zap.String("run_id", e.RunID))
				return nil
			}
			// Edit failed (message deleted/expired): fall through to a new post.
		}
	}

	msg, err := n.session.WebhookExecute(n.webhookID, n.webhookToken, true, &discordgo.WebhookParams{
		Username: n.cfg.Username,
		Content:  content,
		Embeds:   embeds,
	}, discordgo.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("discord webhook execute: %w", err)
	}
	n.rememberMessage(e, msg)
	return nil
}

// sendBot posts (or, in upsert mode, edits) a message via the bot token API.
func (n *Notifier) sendBot(ctx context.Context, e domain.Event) error {
	content, embeds, err := n.render(e)
	if err != nil {
		return err
	}

	// Thread grouped: first event posts top-level, subsequent events reply
	// in the thread. Runs before upsert — the two modes are mutually exclusive.
	if n.canThreadGroup() {
		msgSend := &discordgo.MessageSend{
			Content: content,
			Embeds:  embeds,
		}
		if id, ok := n.cfg.MessageStore.Load(e.RunID); ok && id != "" {
			msgSend.Reference = &discordgo.MessageReference{
				MessageID: id,
			}
		}
		msg, perr := n.session.ChannelMessageSendComplex(n.cfg.ChannelID, msgSend, discordgo.WithContext(ctx))
		if perr != nil {
			return fmt.Errorf("discord send message: %w", perr)
		}
		if e.RunID != "" && msg != nil && msg.ID != "" {
			n.cfg.MessageStore.Save(e.RunID, msg.ID)
		}
		return nil
	}

	if n.canUpsert() {
		if id, ok := n.cfg.MessageStore.Load(e.RunID); ok && id != "" {
			edit := discordgo.NewMessageEdit(n.cfg.ChannelID, id)
			if content != "" {
				edit.Content = &content
			}
			if len(embeds) > 0 {
				edit.Embeds = &embeds
			}
			if _, eerr := n.session.ChannelMessageEditComplex(edit, discordgo.WithContext(ctx)); eerr == nil {
				n.log.Debug("discord message edited", zap.String("run_id", e.RunID))
				return nil
			}
		}
	}

	msg, err := n.session.ChannelMessageSendComplex(n.cfg.ChannelID, &discordgo.MessageSend{
		Content: content,
		Embeds:  embeds,
	}, discordgo.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("discord send message: %w", err)
	}
	n.rememberMessage(e, msg)
	return nil
}

// rememberMessage stores the created message ID for upsert mode.
func (n *Notifier) rememberMessage(e domain.Event, msg *discordgo.Message) {
	if n.canUpsert() && e.RunID != "" && msg != nil && msg.ID != "" {
		n.cfg.MessageStore.Save(e.RunID, msg.ID)
	}
}

// render produces either template-driven content or a default embed.
func (n *Notifier) render(e domain.Event) (string, []*discordgo.MessageEmbed, error) {
	var content string
	if n.tmpl != nil {
		var buf bytes.Buffer
		if err := n.tmpl.Execute(&buf, e); err != nil {
			return "", nil, fmt.Errorf("template execution failed: %w", err)
		}
		content = buf.String()
	}

	mentions := n.roleMentions()
	if mentions != "" {
		if content != "" {
			content = mentions + " " + content
		} else {
			content = mentions
		}
	}

	if n.tmpl != nil {
		return content, nil, nil
	}
	return content, []*discordgo.MessageEmbed{n.embed(e)}, nil
}

// roleMentions formats MentionRoles as "<@&ROLE_ID>" separated by spaces.
func (n *Notifier) roleMentions() string {
	if len(n.cfg.MentionRoles) == 0 {
		return ""
	}
	parts := make([]string, len(n.cfg.MentionRoles))
	for i, id := range n.cfg.MentionRoles {
		parts[i] = "<@&" + id + ">"
	}
	return strings.Join(parts, " ")
}

// embed builds the default-format Discord embed for an event.
func (n *Notifier) embed(e domain.Event) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{
		{Name: "State", Value: fmt.Sprintf("`%s`", e.State), Inline: true},
		{Name: "Run", Value: fmt.Sprintf("`%s`", e.RunName), Inline: true},
	}
	if e.CommitSHA != "" {
		sha := e.CommitSHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		fields = append(fields, &discordgo.MessageEmbedField{Name: fieldCommit, Value: fmt.Sprintf("`%s`", sha), Inline: true})
	}

	embed := &discordgo.MessageEmbed{
		Title:       e.Context,
		Description: e.Description,
		Color:       colorFor(e.State),
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("%s/%s", e.Namespace, e.RunName)},
	}
	if e.TargetURL != "" {
		embed.URL = e.TargetURL
	}
	return embed
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
