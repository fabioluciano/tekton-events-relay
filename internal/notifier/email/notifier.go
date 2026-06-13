// Package email implements the Notifier for plain SMTP delivery.
// Supports implicit TLS (465), STARTTLS (587, default) and unencrypted
// in-cluster relays (25). Subject and body are Go templates rendered
// against the event.
package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"text/template"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
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

	defaultSubjectTemplate = `[tekton] {{ if .PipelineName }}{{ .PipelineName }}{{ else }}{{ .RunName }}{{ end }} — {{ .State }}`

	defaultBodyTemplate = `Pipeline {{ .State }}: {{ if .PipelineName }}{{ .PipelineName }}{{ else }}{{ .RunName }}{{ end }}

Run:       {{ .RunName }}
Namespace: {{ .Namespace }}
{{- if .CommitSHA }}
Commit:    {{ printf "%.8s" .CommitSHA }}
{{- end }}
{{- if .Context }}
Context:   {{ .Context }}
{{- end }}
{{- if .Description }}

{{ .Description }}
{{- end }}
{{- if .Results }}

Results:
{{- range .Results }}
- {{ .Name }}: {{ .Value }}
{{- end }}
{{- end }}
{{- if .TargetURL }}

View logs: {{ .TargetURL }}
{{- end }}
`
)

// Config holds the email notifier configuration.
type Config struct {
	Name       string
	Host       string
	Port       int    // default 587
	Encryption string // starttls (default) | tls | none
	Username   string // empty = no AUTH
	Password   string
	From       string
	To         []string
	Subject    string // Go template; default: defaultSubjectTemplate
	Template   string // body Go template; default: defaultBodyTemplate
	// TemplateFile takes precedence over Template when set.
	TemplateFile       string
	HTML               bool // send body as text/html instead of text/plain
	Timeout            time.Duration
	InsecureSkipVerify bool // skip TLS verification (self-hosted relays)
}

// Notifier sends pipeline events as email via SMTP.
type Notifier struct {
	cfg         Config
	subjectTmpl *template.Template
	bodyTmpl    *template.Template
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

	subjectSrc := cfg.Subject
	if subjectSrc == "" {
		subjectSrc = defaultSubjectTemplate
	}
	subjectTmpl, err := template.New("subject").Parse(subjectSrc)
	if err != nil {
		return nil, fmt.Errorf("email %s: invalid subject template: %w", cfg.Name, err)
	}

	bodySrc := cfg.Template
	if cfg.TemplateFile != "" {
		data, err := os.ReadFile(cfg.TemplateFile)
		if err != nil {
			return nil, fmt.Errorf("email %s: read template file %s: %w", cfg.Name, cfg.TemplateFile, err)
		}
		bodySrc = string(data)
	}
	if bodySrc == "" {
		bodySrc = defaultBodyTemplate
	}
	bodyTmpl, err := template.New("body").Parse(bodySrc)
	if err != nil {
		return nil, fmt.Errorf("email %s: invalid template: %w", cfg.Name, err)
	}

	return &Notifier{cfg: cfg, subjectTmpl: subjectTmpl, bodyTmpl: bodyTmpl, log: log}, nil
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

	msg := n.buildMessage(sanitizeHeader(subject.String()), body.String())
	if err := n.send(ctx, msg); err != nil {
		return fmt.Errorf("email %s: %w", n.cfg.Name, err)
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

func (n *Notifier) buildMessage(subject, body string) []byte {
	contentType := "text/plain; charset=UTF-8"
	if n.cfg.HTML {
		contentType = "text/html; charset=UTF-8"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", n.cfg.From)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(n.cfg.To, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: %s\r\n", contentType)
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return []byte(b.String())
}

// send dials the SMTP server honoring ctx and the configured encryption,
// authenticates when credentials are present, and submits the message.
func (n *Notifier) send(ctx context.Context, msg []byte) error {
	addr := net.JoinHostPort(n.cfg.Host, fmt.Sprintf("%d", n.cfg.Port))

	dialCtx, cancel := context.WithTimeout(ctx, n.cfg.Timeout)
	defer cancel()

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	// Bound the whole SMTP dialog, not just the dial.
	if deadline, ok := dialCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	tlsCfg := &tls.Config{
		ServerName:         n.cfg.Host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: n.cfg.InsecureSkipVerify, // #nosec G402 -- explicit opt-in from config
	}

	if n.cfg.Encryption == EncryptionTLS {
		conn = tls.Client(conn, tlsCfg)
	}

	client, err := smtp.NewClient(conn, n.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer func() { _ = client.Close() }()

	if n.cfg.Encryption == EncryptionSTARTTLS {
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if n.cfg.Username != "" {
		auth := smtp.PlainAuth("", n.cfg.Username, n.cfg.Password, n.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	if err := client.Mail(n.cfg.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	for _, rcpt := range n.cfg.To {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt %s: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close message: %w", err)
	}
	return client.Quit()
}
