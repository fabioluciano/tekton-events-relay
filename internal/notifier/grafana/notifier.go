// Package grafana posts deployment markers to the Grafana Annotations API,
// so pipeline events appear as vertical markers on dashboards.
package grafana

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// Notifier posts annotations to Grafana.
type Notifier struct {
	base     *notifier.Base
	name     string
	tags     []string
	template *template.Template
}

// Config configures the Grafana notifier.
type Config struct {
	Name string
	URL  string // Grafana base URL (e.g. https://grafana.example.com)
	// Token provides the service account / OAuth2 bearer token, resolved fresh
	// per request so rotated secrets and refreshed OAuth2 tokens are picked up.
	Token    scm.TokenRefresher
	Tags     []string
	Template string
	Log      *zap.Logger
}

// validateURL checks that a URL has an http or https scheme.
func validateURL(urlStr string) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", u.Scheme)
	}
}

// New creates a Grafana annotations notifier.
func New(cfg Config) (*Notifier, error) {
	if err := validateURL(cfg.URL); err != nil {
		return nil, fmt.Errorf("invalid Grafana URL: %w", err)
	}

	if cfg.Template == "" {
		return nil, fmt.Errorf("grafana: template is required (must be provided via ConfigMap)")
	}
	tmpl, err := scm.CompileTemplate("grafana", cfg.Template, nil)
	if err != nil {
		return nil, fmt.Errorf("compile template: %w", err)
	}

	if cfg.Token == nil {
		return nil, fmt.Errorf("grafana: token refresher is required")
	}

	n := &Notifier{name: cfg.Name, tags: cfg.Tags, template: tmpl}
	url := strings.TrimRight(cfg.URL, "/") + "/api/annotations"
	token := cfg.Token

	n.base = &notifier.Base{
		HTTP:      notifier.DefaultHTTPClient(),
		UserAgent: notifier.UserAgent,
		Log:       cfg.Log,
		BuildURL:  func(domain.Event) (string, error) { return url, nil },
		Auth: func(req *http.Request) error {
			tok, err := token.Token(req.Context())
			if err != nil {
				return fmt.Errorf("grafana: resolve token: %w", err)
			}
			req.Header.Set("Authorization", "Bearer "+tok)
			return nil
		},
		BuildPayload: func(e domain.Event) (any, error) {
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, e); err != nil {
				return nil, fmt.Errorf("execute template: %w", err)
			}
			tags := append([]string{"tekton-events-relay", string(e.State)}, n.tags...)
			return map[string]any{
				"time": annotationTime(e),
				"text": buf.String(),
				"tags": tags,
			}, nil
		},
	}
	return n, nil
}

// annotationTime uses the run finish time when available (epoch millis).
func annotationTime(e domain.Event) int64 {
	t := e.FinishedAt
	if t.IsZero() {
		t = time.Now()
	}
	return t.UnixMilli()
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return n.name }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle posts the annotation.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	return n.base.Send(ctx, e)
}
