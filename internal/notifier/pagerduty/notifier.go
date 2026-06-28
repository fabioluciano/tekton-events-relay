// Package pagerduty implements the Notifier for PagerDuty via Events API v2.
// Doc: https://developer.pagerduty.com/docs/events-api-v2/trigger-events/
//
// Behavior:
//   - StateFailure / StateError  → trigger (opens incident)
//   - StateSuccess               → resolve (closes open incident, if any)
//   - StateRunning               → acknowledge (opt-in via AcknowledgeOnRunning)
//   - other states               → ignored by default
//
// Incident deduplication uses RunID as DedupKey — ensures that
// multiple events from the same run don't open multiple incidents.
// acknowledge and resolve both reference the original incident via dedup_key.
package pagerduty

import (
	"go.uber.org/zap"

	"context"
	"fmt"
	"net/http"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	eventsAPI         = "https://events.pagerduty.com/v2/enqueue"
	actionTrigger     = "trigger"
	actionResolve     = "resolve"
	actionAcknowledge = "acknowledge"
)

// Config contains PagerDuty integration settings.
type Config struct {
	IntegrationKey       scm.TokenRefresher // PagerDuty service routing key
	Severity             string             // critical, error, warning, info — default: critical
	AcknowledgeOnRunning bool               // when true, in-progress (running) events send an acknowledge
}

// Notifier sends events to PagerDuty.
type Notifier struct {
	base *notifier.Base
	cfg  Config
}

// New creates a PagerDuty notifier.
func New(cfg Config, log *zap.Logger) *Notifier {
	if cfg.Severity == "" {
		cfg.Severity = "critical"
	}
	n := &Notifier{cfg: cfg}
	n.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildURL:     func(_ domain.Event) (string, error) { return eventsAPI, nil },
		BuildPayload: func(e domain.Event) (any, error) { return n.payload(e, "") },
		Auth:         func(_ *http.Request) error { return nil }, // PagerDuty auth goes in payload
		UserAgent:    notifier.UserAgent,
		Log:          log,
	}
	return n
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "pagerduty" }

// Type returns the action type for generic notifiers.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle sends an event to PagerDuty.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	action := actionFor(e.State, n.cfg.AcknowledgeOnRunning)
	if action == "" {
		return nil // irrelevant state, ignore
	}
	if n.cfg.IntegrationKey == nil {
		return fmt.Errorf("pagerduty: integration key refresher is required")
	}
	integrationKey, err := n.cfg.IntegrationKey.Token(ctx)
	if err != nil {
		return fmt.Errorf("pagerduty: resolve integration key: %w", err)
	}
	base := *n.base
	base.BuildPayload = func(e domain.Event) (any, error) {
		return n.payload(e, integrationKey)
	}
	return base.Send(ctx, e)
}

func actionFor(s domain.State, acknowledgeOnRunning bool) string {
	switch s {
	case domain.StateFailure, domain.StateError:
		return actionTrigger
	case domain.StateSuccess:
		return actionResolve
	case domain.StateRunning:
		if acknowledgeOnRunning {
			return actionAcknowledge
		}
	}
	return ""
}

func (n *Notifier) payload(e domain.Event, integrationKey string) (any, error) {
	action := actionFor(e.State, n.cfg.AcknowledgeOnRunning)
	if action == "" {
		return nil, fmt.Errorf("unsupported state for pagerduty: %s", e.State)
	}

	p := map[string]any{
		"routing_key":  integrationKey,
		"event_action": action,
		"dedup_key":    e.RunID, // ensures idempotency per run (UID is unique across time)
		"payload": map[string]any{
			"summary":   fmt.Sprintf("[%s] %s — %s", e.State, e.Context, e.Description),
			"source":    fmt.Sprintf("%s/%s", e.Namespace, e.RunName),
			"severity":  n.cfg.Severity,
			"component": e.Context,
			"group":     e.Namespace,
			"custom_details": map[string]string{
				"run_id":     e.RunName,
				"namespace":  e.Namespace,
				"commit_sha": e.CommitSHA,
				"target_url": e.TargetURL,
			},
		},
	}

	if e.TargetURL != "" {
		p["links"] = []map[string]string{
			{"href": e.TargetURL, "text": "View run"},
		}
	}

	return p, nil
}
