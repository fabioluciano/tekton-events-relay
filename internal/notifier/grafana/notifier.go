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
	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// Compile-time checks.
var _ notifier.ActionHandler = (*Notifier)(nil)

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
	Token        scm.TokenRefresher
	Tags         []string
	Template     string
	DashboardUID string
	PanelID      int
	Log          *zap.Logger
	// HTTPClient overrides the HTTP client. When nil, httpx.NewClient() is used.
	HTTPClient *http.Client
	// RetryPolicy overrides the global retry policy. When nil, the global default is used.
	RetryPolicy *httpx.RetryPolicy
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

	httpClient := httpx.NewClient()
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}
	n.base = &notifier.Base{
		HTTP:        httpClient,
		UserAgent:   notifier.UserAgent,
		Log:         cfg.Log,
		RetryPolicy: cfg.RetryPolicy,
		BuildURL:    func(domain.Event) (string, error) { return url, nil },
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
			start, end := annotationTimes(e)
			payload := map[string]any{
				"time": start,
				"text": buf.String(),
				"tags": tags,
			}
			if end != nil {
				payload["timeEnd"] = *end
			}
			if cfg.DashboardUID != "" {
				payload["dashboardUID"] = cfg.DashboardUID
			}
			if cfg.PanelID != 0 {
				payload["panelId"] = cfg.PanelID
			}
			return payload, nil
		},
	}
	return n, nil
}

// annotationTimes returns the annotation start time and, for terminal runs that
// carry a finish time, the region end time (both epoch millis). The second
// return is nil for point annotations: a non-terminal event keeps its existing
// behavior of a single timestamp at the current moment.
func annotationTimes(e domain.Event) (int64, *int64) {
	if e.FinishedAt.IsZero() {
		return time.Now().UnixMilli(), nil
	}
	end := e.FinishedAt.UnixMilli()
	start := end
	if !e.StartedAt.IsZero() {
		start = e.StartedAt.UnixMilli()
	}
	return start, &end
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return n.name }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle posts the annotation.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	return n.base.Send(ctx, e)
}
