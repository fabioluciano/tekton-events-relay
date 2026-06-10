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

	"context"
	"fmt"
	"net/http"

	"github.com/itchyny/gojq"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

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

	n.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildURL:     func(_ domain.Event) (string, error) { return cfg.URL, nil },
		BuildPayload: n.payload,
		Auth:         n.auth,
		UserAgent:    notifier.UserAgent,
		Log:          log,
	}
	return n, nil
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "webhook" }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle sends the event via webhook.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	return n.base.Send(ctx, e)
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

func (n *Notifier) auth(req *http.Request) error {
	if err := applyAuth(req, n.cfg.Auth); err != nil {
		return fmt.Errorf("apply webhook auth: %w", err)
	}
	for k, v := range n.cfg.Headers {
		req.Header.Set(k, v)
	}
	return nil
}
