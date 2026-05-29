// Package webhook implements a generic Notifier via HTTP.
// Useful as fan-out to any system not covered by specific adapters
// — just point to any HTTP endpoint.
//
// The sent payload is the domain.Event serialized as JSON with
// additional context fields. Custom headers allow authentication
// in any format.
package webhook

import (
	"context"
	"net/http"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// Config holds the configuration for the webhook notifier.
type Config struct {
	URL      string
	Headers  map[string]string // custom headers (Authorization, X-Token, etc.)
	NotifyOn []string          // default: all states
}

// Notifier sends events to a generic HTTP webhook endpoint.
type Notifier struct {
	base *notifier.Base
	cfg  Config
}

func New(cfg Config) *Notifier {
	n := &Notifier{cfg: cfg}
	n.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildURL:     func(_ domain.Event) (string, error) { return cfg.URL, nil },
		BuildPayload: n.payload,
		Auth:         n.auth,
		UserAgent:    notifier.UserAgent,
	}
	return n
}

func (n *Notifier) Name() string { return "webhook" }

func (n *Notifier) Notify(ctx context.Context, e domain.Event) error {
	if !notifier.ShouldNotify(n.cfg.NotifyOn, e.State) {
		return nil
	}
	return n.base.Send(ctx, e)
}

// payload sends the domain.Event directly as JSON — simple and predictable
// contract for the webhook consumer.
func (n *Notifier) payload(e domain.Event) (any, error) {
	return map[string]any{
		"run_id":      e.RunName,
		"namespace":   e.Namespace,
		"resource":    e.Resource,
		"state":       e.State,
		"context":     e.Context,
		"description": e.Description,
		"target_url":  e.TargetURL,
		"commit_sha":  e.CommitSHA,
		"repo": map[string]string{
			"owner":     e.Repo.Owner,
			"name":      e.Repo.Name,
			"id":        e.Repo.ID,
			"workspace": e.Repo.Workspace,
			"project":   e.Repo.Project,
			"org":       e.Repo.Org,
		},
		"started_at":  e.StartedAt,
		"finished_at": e.FinishedAt,
	}, nil
}

func (n *Notifier) auth(req *http.Request) {
	for k, v := range n.cfg.Headers {
		req.Header.Set(k, v)
	}
}
