// Package honeycomb implements the Notifier for Honeycomb via the Events API.
// Sends pipeline events as custom events to a Honeycomb dataset for querying
// and alerting.
// Doc: https://docs.honeycomb.io/send-data/events/
package honeycomb

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const notifierName = "honeycomb"

// Config holds the Honeycomb notifier configuration.
type Config struct {
	APIKey  scm.TokenRefresher
	Dataset string
	// HTTPClient overrides the HTTP client. When nil, notifier.DefaultHTTPClient() is used.
	HTTPClient *http.Client
	// RetryPolicy overrides the global retry policy. When nil, the global default is used.
	RetryPolicy *httpx.RetryPolicy
}

// Notifier sends events to Honeycomb via the Events API.
type Notifier struct {
	base *notifier.Base
	cfg  Config
}

// New creates a new Honeycomb notifier with the given configuration.
func New(cfg Config, log *zap.Logger) *Notifier {
	n := &Notifier{cfg: cfg}
	httpClient := notifier.DefaultHTTPClient()
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}
	n.base = &notifier.Base{
		HTTP:         httpClient,
		BuildURL:     n.buildURL,
		BuildPayload: n.buildPayload,
		Auth:         n.auth,
		UserAgent:    notifier.UserAgent,
		Log:          log,
		RetryPolicy:  cfg.RetryPolicy,
	}
	return n
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return notifierName }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle sends the event to Honeycomb.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	return n.base.Send(ctx, e)
}

func (n *Notifier) buildURL(_ domain.Event) (string, error) {
	return fmt.Sprintf("https://api.honeycomb.io/1/events/%s", n.cfg.Dataset), nil
}

func (n *Notifier) auth(req *http.Request) error {
	if n.cfg.APIKey == nil {
		return fmt.Errorf("honeycomb: api key refresher is required")
	}
	apiKey, err := n.cfg.APIKey.Token(req.Context())
	if err != nil {
		return fmt.Errorf("honeycomb: resolve api key: %w", err)
	}
	req.Header.Set("X-Honeycomb-Team", apiKey)
	return nil
}

func (n *Notifier) buildPayload(e domain.Event) (any, error) {
	payload := map[string]any{
		"runName":      e.RunName,
		"state":        string(e.State),
		"provider":     e.Provider,
		"namespace":    e.Namespace,
		"runID":        e.RunID,
		"resource":     string(e.Resource),
		"pipelineName": e.PipelineName,
		"taskName":     e.TaskName,
		"context":      e.Context,
		"description":  e.Description,
		"userAgent":    notifier.UserAgent,
	}

	if !e.StartedAt.IsZero() {
		payload["startedAt"] = e.StartedAt.Format(time.RFC3339)
	}
	if !e.FinishedAt.IsZero() {
		payload["finishedAt"] = e.FinishedAt.Format(time.RFC3339)
	}
	if e.CommitSHA != "" {
		payload["commitSHA"] = e.CommitSHA
	}
	if e.TargetURL != "" {
		payload["targetURL"] = e.TargetURL
	}

	return payload, nil
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (n *Notifier) Close() error { return nil }
