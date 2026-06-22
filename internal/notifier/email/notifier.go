// Package email implements the Notifier for SMTP delivery via the
// github.com/wneessen/go-mail SDK. It supports implicit TLS (465),
// STARTTLS (587, default) and unencrypted in-cluster relays (25), CC/BCC,
// Reply-To, message threading (In-Reply-To/References keyed by RunID) and
// two authentication modes: SMTP PLAIN (username/password) and XOAUTH2
// (per-request access token from a TokenRefresher). Subject and body are
// Go templates rendered against the event.
package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"text/template"
	"time"

	mail "github.com/wneessen/go-mail"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/msgstore"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	notifierName = "email"

	// EncryptionSTARTTLS upgrades the connection after EHLO (port 587).
	EncryptionSTARTTLS = "starttls"
	// EncryptionTLS opens an implicit TLS connection (port 465).
	EncryptionTLS = "tls"
	// EncryptionNone sends in clear text (in-cluster relays only).
	EncryptionNone = "none"

	defaultPort    = 587
	defaultTimeout = 15 * time.Second
)

// Config holds the email notifier configuration.
type Config struct {
	Name       string
	Host       string
	Port       int    // default 587
	Encryption string // starttls (default) | tls | none
	Username   string // empty = no AUTH (PLAIN); required for XOAUTH2
	Password   string // PLAIN auth password
	From       string
	To         []string
	Cc         []string
	Bcc        []string
	ReplyTo    string
	Subject    string // Go template (must be provided via ConfigMap)
	Template   string // body Go template (must be provided via ConfigMap)
	HTML       bool   // send body as text/html instead of text/plain
	Timeout    time.Duration
	// InsecureSkipVerify skips TLS verification (self-hosted relays).
	InsecureSkipVerify bool
	// XOAuth2 selects the XOAUTH2 SASL mechanism instead of PLAIN. The access
	// token is fetched from Token on every send (rotation/refresh-safe).
	XOAuth2 bool
	// Token supplies the XOAUTH2 access token per request. Required when
	// XOAuth2 is true; ignored otherwise.
	Token scm.TokenRefresher
	// Store persists the first Message-ID per RunID for threading. When nil a
	// per-pod in-memory store is created.
	Store msgstore.Store
}

// Notifier sends pipeline events as email via SMTP.
type Notifier struct {
	cfg         Config
	subjectTmpl *template.Template
	bodyTmpl    *template.Template
	store       msgstore.Store
	log         *zap.Logger
}

// New creates a new email notifier with the given configuration.
func New(cfg Config, log *zap.Logger) (*Notifier, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("email %s: host is required", cfg.Name)
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("email %s: from is required", cfg.Name)
	}
	if len(cfg.To) == 0 {
		return nil, fmt.Errorf("email %s: at least one recipient is required", cfg.Name)
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if cfg.Encryption == "" {
		cfg.Encryption = EncryptionSTARTTLS
	}
	switch cfg.Encryption {
	case EncryptionSTARTTLS, EncryptionTLS, EncryptionNone:
	default:
		return nil, fmt.Errorf("email %s: invalid encryption %q (starttls, tls or none)", cfg.Name, cfg.Encryption)
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.XOAuth2 {
		if cfg.Username == "" {
			return nil, fmt.Errorf("email %s: username is required for xoauth2", cfg.Name)
		}
		if cfg.Token == nil {
			return nil, fmt.Errorf("email %s: token source is required for xoauth2", cfg.Name)
		}
	}

	// Subject and body templates must come from ConfigMap (chart default or configmapRef).
	// LoadTemplateString resolves paths mounted at /etc/templates or inline strings.
	subjectSrc, err := scm.LoadTemplateString(cfg.Subject)
	if err != nil {
		return nil, fmt.Errorf("email %s: load subject template: %w", cfg.Name, err)
	}
	if subjectSrc == "" {
		return nil, fmt.Errorf("email %s: subject template is required (must be provided via ConfigMap)", cfg.Name)
	}
	subjectTmpl, err := template.New("subject").Parse(subjectSrc)
	if err != nil {
		return nil, fmt.Errorf("email %s: invalid subject template: %w", cfg.Name, err)
	}

	bodySrc, err := scm.LoadTemplateString(cfg.Template)
	if err != nil {
		return nil, fmt.Errorf("email %s: load template: %w", cfg.Name, err)
	}
	if bodySrc == "" {
		return nil, fmt.Errorf("email %s: body template is required (must be provided via ConfigMap)", cfg.Name)
	}
	bodyTmpl, err := template.New("body").Parse(bodySrc)
	if err != nil {
		return nil, fmt.Errorf("email %s: invalid template: %w", cfg.Name, err)
	}
	if log == nil {
		log = zap.NewNop()
	}
	store := cfg.Store
	if store == nil {
		store = msgstore.NewMemoryStore(0, 0)
	}

	return &Notifier{cfg: cfg, subjectTmpl: subjectTmpl, bodyTmpl: bodyTmpl, store: store, log: log}, nil
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return notifierName }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle renders the message and delivers it via SMTP.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	var subject, body strings.Builder
	if err := n.subjectTmpl.Execute(&subject, e); err != nil {
		return fmt.Errorf("email %s: render subject: %w", n.cfg.Name, err)
	}
	if err := n.bodyTmpl.Execute(&body, e); err != nil {
		return fmt.Errorf("email %s: render body: %w", n.cfg.Name, err)
	}

	msg, threadKey, msgID, err := n.buildMessage(sanitizeHeader(subject.String()), body.String(), e)
	if err != nil {
		return fmt.Errorf("email %s: %w", n.cfg.Name, err)
	}
	if err := n.send(ctx, msg); err != nil {
		return fmt.Errorf("email %s: %w", n.cfg.Name, err)
	}
	// Record the root Message-ID for this run only on the first successful
	// send, so subsequent state-change emails thread under it.
	if threadKey != "" {
		if _, ok := n.store.Load(threadKey); !ok {
			n.store.Save(threadKey, msgID)
		}
	}
	n.log.Debug("email sent",
		zap.String("instance", n.cfg.Name),
		zap.Strings("to", n.cfg.To),
		zap.String("run", e.RunName))
	return nil
}

// sanitizeHeader strips CR/LF so event-derived text (PR titles etc.) cannot
// inject extra SMTP headers.
func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// threadKey returns the per-run key used to thread state-change emails. RunID
// is preferred; RunName is the fallback when the decoder did not populate it.
func threadKey(e domain.Event) string {
	if e.RunID != "" {
		return e.RunID
	}
	return e.RunName
}

// buildMessage assembles a go-mail message with recipients, body, Reply-To and
// threading headers. It returns the message, the thread key and the generated
// Message-ID (with angle brackets) so the caller can persist it after a
// successful send.
func (n *Notifier) buildMessage(subject, body string, e domain.Event) (*mail.Msg, string, string, error) {
	msg := mail.NewMsg()
	if err := msg.From(n.cfg.From); err != nil {
		return nil, "", "", fmt.Errorf("from %q: %w", n.cfg.From, err)
	}
	if err := msg.To(n.cfg.To...); err != nil {
		return nil, "", "", fmt.Errorf("to: %w", err)
	}
	if len(n.cfg.Cc) > 0 {
		if err := msg.Cc(n.cfg.Cc...); err != nil {
			return nil, "", "", fmt.Errorf("cc: %w", err)
		}
	}
	if len(n.cfg.Bcc) > 0 {
		// go-mail keeps Bcc out of the rendered headers; it is added to the
		// envelope (RCPT TO) only.
		if err := msg.Bcc(n.cfg.Bcc...); err != nil {
			return nil, "", "", fmt.Errorf("bcc: %w", err)
		}
	}
	if n.cfg.ReplyTo != "" {
		if err := msg.ReplyTo(n.cfg.ReplyTo); err != nil {
			return nil, "", "", fmt.Errorf("reply-to %q: %w", n.cfg.ReplyTo, err)
		}
	}

	msg.Subject(subject)
	msg.SetDate()
	msg.SetMessageID()
	msgID := msg.GetMessageID()

	key := threadKey(e)
	if key != "" {
		if root, ok := n.store.Load(key); ok && root != "" {
			msg.SetGenHeader(mail.HeaderInReplyTo, root)
			msg.SetGenHeader(mail.HeaderReferences, root)
		}
	}

	contentType := mail.TypeTextPlain
	if n.cfg.HTML {
		contentType = mail.TypeTextHTML
	}
	msg.SetBodyString(contentType, body)
	return msg, key, msgID, nil
}

// tlsConfig builds the TLS configuration shared by STARTTLS and implicit TLS.
func (n *Notifier) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName:         n.cfg.Host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: n.cfg.InsecureSkipVerify, // #nosec G402 -- explicit opt-in from config
	}
}

// clientOptions resolves the go-mail client options for the configured
// encryption and authentication mode. XOAUTH2 fetches a fresh access token
// from the TokenRefresher so the client is rebuilt per send.
func (n *Notifier) clientOptions(ctx context.Context) ([]mail.Option, error) {
	opts := []mail.Option{
		mail.WithPort(n.cfg.Port),
		mail.WithTimeout(n.cfg.Timeout),
		mail.WithTLSConfig(n.tlsConfig()),
	}

	switch n.cfg.Encryption {
	case EncryptionTLS:
		opts = append(opts, mail.WithSSL())
	case EncryptionSTARTTLS:
		opts = append(opts, mail.WithTLSPortPolicy(mail.TLSMandatory))
	case EncryptionNone:
		opts = append(opts, mail.WithTLSPortPolicy(mail.NoTLS))
	}

	switch {
	case n.cfg.XOAuth2:
		token, err := n.cfg.Token.Token(ctx)
		if err != nil {
			return nil, fmt.Errorf("xoauth2 token: %w", err)
		}
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthXOAUTH2),
			mail.WithUsername(n.cfg.Username),
			mail.WithPassword(token),
		)
	case n.cfg.Username != "":
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(n.cfg.Username),
			mail.WithPassword(n.cfg.Password),
		)
	}

	return opts, nil
}

// send builds a go-mail client honoring ctx and the configured encryption and
// authentication mode, then submits the message.
func (n *Notifier) send(ctx context.Context, msg *mail.Msg) error {
	dialCtx, cancel := context.WithTimeout(ctx, n.cfg.Timeout)
	defer cancel()

	opts, err := n.clientOptions(dialCtx)
	if err != nil {
		return err
	}

	client, err := mail.NewClient(n.cfg.Host, opts...)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}

	if err := client.DialAndSendWithContext(dialCtx, msg); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}
