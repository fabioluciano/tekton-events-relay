// Package grafana posts deployment markers to the Grafana Annotations API,
// so pipeline events appear as vertical markers on dashboards.
package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	base          *notifier.Base
	name          string
	tags          []string
	template      *template.Template
	dashboardUIDs []string
	panelID       int
	token         scm.TokenRefresher
	baseURL       string
	log           *zap.Logger
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
	// DashboardUIDs creates one annotation per UID in parallel. When set it
	// overrides DashboardUID.
	DashboardUIDs []string
	PanelID       int
	Log           *zap.Logger
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

	// Normalize: singular DashboardUID becomes a one-element DashboardUIDs
	// when DashboardUIDs is not explicitly set.
	dashboardUIDs := cfg.DashboardUIDs
	if len(dashboardUIDs) == 0 && cfg.DashboardUID != "" {
		dashboardUIDs = []string{cfg.DashboardUID}
	}

	n := &Notifier{
		name:          cfg.Name,
		tags:          cfg.Tags,
		template:      tmpl,
		dashboardUIDs: dashboardUIDs,
		panelID:       cfg.PanelID,
		token:         cfg.Token,
		baseURL:       strings.TrimRight(cfg.URL, "/"),
		log:           cfg.Log,
	}
	if n.log == nil {
		n.log = zap.NewNop()
	}
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
			uid := ""
			if len(n.dashboardUIDs) == 1 {
				uid = n.dashboardUIDs[0]
			}
			return n.buildPayload(e, uid)
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

// Handle posts the annotation. When DashboardUIDs is set, one annotation per
// UID is created in parallel.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	if len(n.dashboardUIDs) <= 1 {
		return n.base.Send(ctx, e)
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, uid := range n.dashboardUIDs {
		wg.Add(1)
		go func(dashUID string) {
			defer wg.Done()
			if err := n.sendForDashboard(ctx, e, dashUID); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(uid)
	}
	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("grafana: %d dashboard annotation(s) failed: %w", len(errs), errs[0])
	}
	return nil
}

// sendForDashboard sends a single annotation scoped to the given dashboard UID.
func (n *Notifier) sendForDashboard(ctx context.Context, e domain.Event, dashboardUID string) error {
	payload, err := n.buildPayload(e, dashboardUID)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	u, err := n.base.BuildURL(e)
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", n.base.UserAgent)
	if err := n.base.Auth(req); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	resp, err := n.base.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("grafana returned %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) buildPayload(e domain.Event, dashboardUID string) (any, error) {
	var buf bytes.Buffer
	if err := n.template.Execute(&buf, e); err != nil {
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
	if dashboardUID != "" {
		payload["dashboardUID"] = dashboardUID
	}
	if n.panelID != 0 {
		payload["panelId"] = n.panelID
	}
	return payload, nil
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (n *Notifier) Close() error { return nil }
