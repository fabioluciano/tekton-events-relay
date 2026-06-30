// Package newrelic implements the Notifier for New Relic via the Event API.
// Creates custom events in New Relic that can be queried with NRQL and
// used in dashboards and alerts.
// Doc: https://docs.newrelic.com/docs/data-apis/ingest-apis/event-api/introduction-event-api/
package newrelic

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

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

const (
	notifierName       = "newrelic"
	defaultInsightsURL = "https://insights-collector.newrelic.com"
	eventType          = "TektonPipelineEvent"
)

// Config holds the New Relic notifier configuration.
type Config struct {
	APIKey    scm.TokenRefresher
	AccountID string // New Relic account ID (required)
	// HTTPClient overrides the HTTP client. When nil, notifier.DefaultHTTPClient() is used.
	HTTPClient *http.Client
	// RetryPolicy overrides the global retry policy. When nil, the global default is used.
	RetryPolicy *httpx.RetryPolicy
}

// Notifier sends events to New Relic via the Event API.
type Notifier struct {
	base *notifier.Base
	cfg  Config
}

// New creates a new New Relic notifier with the given configuration.
func New(cfg Config, log *zap.Logger) *Notifier {
	n := &Notifier{cfg: cfg}
	httpClient := notifier.DefaultHTTPClient()
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}
	n.base = &notifier.Base{
		HTTP:         httpClient,
		BuildURL:     n.url,
		BuildPayload: n.payload,
		Auth:         n.auth,
		UserAgent:    notifier.UserAgent,
		Log:          log,
		RetryPolicy:  cfg.RetryPolicy,
	}
	return n
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return notifierName }

// Provider returns the provider type identifier.
func (n *Notifier) Provider() string { return notifierName }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle sends the event to New Relic.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	return n.base.Send(ctx, e)
}

func (n *Notifier) url(_ domain.Event) (string, error) {
	u := fmt.Sprintf("%s/v1/accounts/%s/events", defaultInsightsURL, n.cfg.AccountID)
	if err := validateURL(u); err != nil {
		return "", fmt.Errorf("invalid New Relic URL: %w", err)
	}
	return u, nil
}

func (n *Notifier) auth(req *http.Request) error {
	if n.cfg.APIKey == nil {
		return fmt.Errorf("newrelic: api key refresher is required")
	}
	apiKey, err := n.cfg.APIKey.Token(req.Context())
	if err != nil {
		return fmt.Errorf("newrelic: resolve api key: %w", err)
	}
	req.Header.Set("X-Insert-Key", apiKey)
	return nil
}

func (n *Notifier) payload(e domain.Event) (any, error) {
	event := map[string]any{
		"eventType": eventType,
		"runName":   e.RunName,
		"state":     string(e.State),
		"namespace": e.Namespace,
		"runID":     e.RunID,
		"resource":  string(e.Resource),
	}

	if e.Provider != "" {
		event["provider"] = e.Provider
	}
	if e.Context != "" {
		event["context"] = e.Context
	}
	if e.Description != "" {
		event["description"] = e.Description
	}
	if e.CommitSHA != "" {
		event["commitSHA"] = e.CommitSHA
	}
	if e.PipelineName != "" {
		event["pipelineName"] = e.PipelineName
	}
	if e.TaskName != "" {
		event["taskName"] = e.TaskName
	}

	// New Relic Event API expects a JSON array of events.
	return []any{event}, nil
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (n *Notifier) Close() error { return nil }
