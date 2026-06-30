// Package webhook implements a generic Notifier via HTTP.
// Useful as fan-out to any system not covered by specific adapters
// — just point to any HTTP endpoint.
//
// The sent payload is the domain.Event serialized as JSON with
// additional context fields. Custom headers allow authentication
// in any format.
package webhook

import (
	"go.uber.org/zap"

	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/itchyny/gojq"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// Compile-time checks.
var _ notifier.ActionHandler = (*Notifier)(nil)

// Payload field names for webhook events.
const (
	PayloadFieldRunID       = "run_id"
	PayloadFieldNamespace   = "namespace"
	PayloadFieldResource    = "resource"
	PayloadFieldState       = "state"
	PayloadFieldContext     = "context"
	PayloadFieldDescription = "description"
	PayloadFieldTargetURL   = "target_url"
	PayloadFieldCommitSHA   = "commit_sha"
	PayloadFieldRepo        = "repo"
	PayloadFieldStartedAt   = "started_at"
	PayloadFieldFinishedAt  = "finished_at"
)

// Config holds the configuration for the webhook notifier.
type Config struct {
	URL       string
	Auth      *ResolvedAuth
	Transform string            // gojq expression to transform payload
	Headers   map[string]string // custom headers (Authorization, X-Token, etc.)
	// HTTPClient overrides the HTTP client. When nil, httpx.NewClient() is used.
	HTTPClient *http.Client
	// RetryPolicy overrides the global retry policy. When nil, the global default is used.
	RetryPolicy *httpx.RetryPolicy
}

// Notifier sends events to a generic HTTP webhook endpoint.
type Notifier struct {
	base      *notifier.Base
	cfg       Config
	transform *gojq.Code // compiled gojq query (nil if no transform)
}

// New creates a new webhook notifier with the given configuration.
// Returns an error if the transform expression cannot be compiled.
func New(cfg Config, log *zap.Logger) (*Notifier, error) {
	n := &Notifier{cfg: cfg}

	// Compile transform if provided
	if cfg.Transform != "" {
		query, err := gojq.Parse(cfg.Transform)
		if err != nil {
			return nil, fmt.Errorf("parse transform expression: %w", err)
		}
		code, err := gojq.Compile(query)
		if err != nil {
			return nil, fmt.Errorf("compile transform expression: %w", err)
		}
		n.transform = code
	}

	httpClient := httpx.NewClient()
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}
	n.base = &notifier.Base{
		HTTP:         httpClient,
		BuildURL:     func(_ domain.Event) (string, error) { return cfg.URL, validateURL(cfg.URL) },
		BuildPayload: n.payload,
		Auth:         n.auth,
		UserAgent:    notifier.UserAgent,
		Log:          log,
		RetryPolicy:  cfg.RetryPolicy,
	}
	return n, nil
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "webhook" }

// Provider returns the provider type identifier.
func (n *Notifier) Provider() string { return "webhook" }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle sends the event via webhook.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	return n.base.Send(ctx, e)
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (n *Notifier) Close() error { return nil }

// Flush sends multiple events as a single JSON array payload to the webhook URL.
// If a gojq transform is configured, each event is transformed independently
// before being wrapped in an array.
func (n *Notifier) Flush(ctx context.Context, events []domain.Event) error {
	if len(events) == 0 {
		return nil
	}

	payloads := make([]any, 0, len(events))
	for _, e := range events {
		p, err := n.payload(e)
		if err != nil {
			return fmt.Errorf("webhook batch payload: %w", err)
		}
		payloads = append(payloads, p)
	}

	url, err := n.base.BuildURL(events[0])
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	body, err := json.Marshal(payloads)
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

	if n.base.Auth != nil {
		if err := n.base.Auth(req); err != nil {
			return fmt.Errorf("apply auth: %w", err)
		}
	}

	rp := httpx.DefaultRetryPolicy()
	if n.base.RetryPolicy != nil {
		rp = *n.base.RetryPolicy
	}
	resp, err := httpx.DoWithRetryPolicy(n.base.HTTP, req, rp)
	if err != nil {
		return fmt.Errorf("webhook batch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("webhook batch responded %d: %s", resp.StatusCode, string(buf))
	}
	return nil
}

// payload builds the webhook payload from the domain.Event.
// If a transform is configured, it applies the gojq expression to the default payload.
func (n *Notifier) payload(e domain.Event) (any, error) {
	// Build default payload map
	defaultPayload := map[string]any{
		PayloadFieldRunID:       e.RunName,
		PayloadFieldNamespace:   e.Namespace,
		PayloadFieldResource:    e.Resource,
		PayloadFieldState:       e.State,
		PayloadFieldContext:     e.Context,
		PayloadFieldDescription: e.Description,
		PayloadFieldTargetURL:   e.TargetURL,
		PayloadFieldCommitSHA:   e.CommitSHA,
		PayloadFieldRepo: map[string]string{
			"owner":     e.Repo.Owner,
			"name":      e.Repo.Name,
			"id":        e.Repo.ID,
			"workspace": e.Repo.Workspace,
			"project":   e.Repo.Project,
			"org":       e.Repo.Org,
		},
		PayloadFieldStartedAt:  e.StartedAt,
		PayloadFieldFinishedAt: e.FinishedAt,
	}

	// Apply transform if configured
	if n.transform != nil {
		return ApplyTransform(n.transform, defaultPayload)
	}

	return defaultPayload, nil
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

func (n *Notifier) auth(req *http.Request) error {
	if err := applyAuth(req, n.cfg.Auth); err != nil {
		return fmt.Errorf("apply webhook auth: %w", err)
	}
	for k, v := range n.cfg.Headers {
		req.Header.Set(k, v)
	}
	return nil
}
