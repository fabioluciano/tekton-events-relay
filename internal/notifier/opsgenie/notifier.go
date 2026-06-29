// Package opsgenie implements the Notifier for Opsgenie via Alert API v2.
// Doc: https://docs.opsgenie.com/docs/alert-api#create-alert
//
// Behavior:
//   - StateFailure / StateError  → create alert (POST /v2/alerts)
//   - StateSuccess               → close alert  (DELETE /v2/alerts/{alias})
//   - other states               → ignored
//
// Alert deduplication uses alias "tekton-relay:{RunID}" — ensures that
// multiple events from the same run don't open multiple alerts.
package opsgenie

import (
	"context"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	createAlertAPI  = "https://api.opsgenie.com/v2/alerts"
	actionCreate    = "create"
	actionClose     = "close"
	aliasPrefix     = "tekton-relay:"
	defaultPriority = "P3"
)

// Config contains Opsgenie integration settings.
type Config struct {
	APIKey      scm.TokenRefresher
	TeamName    string
	Priority    string // P1-P5, default P3
	HTTPClient  *http.Client
	RetryPolicy *httpx.RetryPolicy
}

// Notifier sends events to Opsgenie.
type Notifier struct {
	base *notifier.Base
	cfg  Config
}

// New creates an Opsgenie notifier.
func New(cfg Config, log *zap.Logger) *Notifier {
	if cfg.Priority == "" {
		cfg.Priority = defaultPriority
	}
	n := &Notifier{cfg: cfg}
	httpClient := notifier.DefaultHTTPClient()
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}
	n.base = &notifier.Base{
		HTTP:         httpClient,
		BuildURL:     func(_ domain.Event) (string, error) { return createAlertAPI, nil },
		BuildPayload: func(e domain.Event) (any, error) { return n.payload(e, "") },
		Auth:         func(_ *http.Request) error { return nil },
		Method:       func(_ domain.Event) string { return http.MethodPost },
		UserAgent:    notifier.UserAgent,
		Log:          log,
		RetryPolicy:  cfg.RetryPolicy,
	}
	return n
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "opsgenie" }

// Type returns the action type for generic notifiers.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle sends an event to Opsgenie.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	action := actionFor(e.State)
	if action == "" {
		return nil
	}
	if n.cfg.APIKey == nil {
		return fmt.Errorf("opsgenie: api key refresher is required")
	}
	apiKey, err := n.cfg.APIKey.Token(ctx)
	if err != nil {
		return fmt.Errorf("opsgenie: resolve api key: %w", err)
	}

	base := *n.base
	base.Auth = func(req *http.Request) error {
		req.Header.Set("Authorization", "GenieKey "+apiKey)
		return nil
	}

	if action == actionClose {
		alias := aliasPrefix + e.RunID
		baseURL, urlErr := n.base.BuildURL(e)
		if urlErr != nil {
			return fmt.Errorf("opsgenie: build base url: %w", urlErr)
		}
		base.BuildURL = func(_ domain.Event) (string, error) {
			return fmt.Sprintf("%s/%s?identifierType=alias", baseURL, alias), nil
		}
		base.Method = func(_ domain.Event) string { return http.MethodDelete }
		base.BuildPayload = func(_ domain.Event) (any, error) { return map[string]any{}, nil }
	} else {
		base.BuildPayload = func(e domain.Event) (any, error) { return n.payload(e, apiKey) }
	}

	return base.Send(ctx, e)
}

func actionFor(s domain.State) string {
	switch s {
	case domain.StateFailure, domain.StateError:
		return actionCreate
	case domain.StateSuccess:
		return actionClose
	}
	return ""
}

func (n *Notifier) payload(e domain.Event, _ string) (any, error) {
	alias := aliasPrefix + e.RunID
	p := map[string]any{
		"message":     fmt.Sprintf("[%s] %s — %s", e.State, e.Context, e.Description),
		"alias":       alias,
		"description": fmt.Sprintf("Pipeline %s in namespace %s", e.RunName, e.Namespace),
		"priority":    n.cfg.Priority,
		"source":      fmt.Sprintf("%s/%s", e.Namespace, e.RunName),
		"details": map[string]string{
			"run_id":     e.RunName,
			"run_uid":    e.RunID,
			"namespace":  e.Namespace,
			"state":      string(e.State),
			"commit_sha": e.CommitSHA,
			"target_url": e.TargetURL,
		},
	}

	if n.cfg.TeamName != "" {
		p["responders"] = []map[string]string{
			{"type": "team", "name": n.cfg.TeamName},
		}
	}

	if e.TargetURL != "" {
		p["actions"] = []string{"View Run"}
	}

	return p, nil
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (n *Notifier) Close() error { return nil }
