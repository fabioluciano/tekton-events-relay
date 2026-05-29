// Package pagerduty implements the Notifier for PagerDuty via Events API v2.
// Doc: https://developer.pagerduty.com/docs/events-api-v2/trigger-events/
//
// Behavior:
//   - StateFailure / StateError  → trigger (opens incident)
//   - StateSuccess               → resolve (closes open incident, if any)
//   - other states               → ignored by default
//
// Incident deduplication uses RunID as DedupKey — ensures that
// multiple events from the same run don't open multiple incidents.
package pagerduty

import (
	"context"
	"fmt"
	"net/http"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	eventsAPI     = "https://events.pagerduty.com/v2/enqueue"
	actionTrigger = "trigger"
	actionResolve = "resolve"
)

// Config contains PagerDuty integration settings.
type Config struct {
	IntegrationKey string // PagerDuty service routing key
	Severity       string // critical, error, warning, info — default: critical
}

// Notifier sends events to PagerDuty.
type Notifier struct {
	base *notifier.Base
	cfg  Config
}

// New creates a PagerDuty notifier.
func New(cfg Config) *Notifier {
	if cfg.Severity == "" {
		cfg.Severity = "critical"
	}
	n := &Notifier{cfg: cfg}
	n.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildURL:     func(_ domain.Event) (string, error) { return eventsAPI, nil },
		BuildPayload: n.payload,
		Auth:         func(_ *http.Request) { /* PagerDuty auth goes in payload */ },
		UserAgent:    notifier.UserAgent,
	}
	return n
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return "pagerduty" }

// Notify sends an event to PagerDuty.
func (n *Notifier) Notify(ctx context.Context, e domain.Event) error {
	action := actionFor(e.State)
	if action == "" {
		return nil // irrelevant state, ignore
	}
	return n.base.Send(ctx, e)
}

func actionFor(s domain.State) string {
	switch s {
	case domain.StateFailure, domain.StateError:
		return actionTrigger
	case domain.StateSuccess:
		return actionResolve
	}
	return ""
}

func (n *Notifier) payload(e domain.Event) (any, error) {
	action := actionFor(e.State)
	if action == "" {
		return nil, fmt.Errorf("unsupported state for pagerduty: %s", e.State)
	}

	p := map[string]any{
		"routing_key":  n.cfg.IntegrationKey,
		"event_action": action,
		"dedup_key":    e.RunName, // ensures idempotency per run
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
