// Package datadog implements the Notifier for Datadog via Events API v2.
// Creates an event in the Datadog Event Stream — appears in the deploys timeline
// and can be correlated with metrics/traces.
// Doc: https://docs.datadoghq.com/api/latest/events/#post-an-event
package datadog

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	defaultSite  = "datadoghq.com"
	notifierName = "datadog"
	alertSuccess = "success"
	alertError   = "error"
	alertInfo    = "info"
)

// Config holds the Datadog notifier configuration.
type Config struct {
	APIKey   string
	Site     string   // default: datadoghq.com (alternative: datadoghq.eu)
	Tags     []string // extra tags besides the automatically generated ones
	NotifyOn []string // default: all states
}

// Notifier sends events to Datadog via the Events API.
type Notifier struct {
	base *notifier.Base
	cfg  Config
}

func New(cfg Config) *Notifier {
	if cfg.Site == "" {
		cfg.Site = defaultSite
	}
	n := &Notifier{cfg: cfg}
	n.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildURL:     n.url,
		BuildPayload: n.payload,
		Auth:         n.auth,
		UserAgent:    notifier.UserAgent,
	}
	return n
}

func (n *Notifier) Name() string { return notifierName }

func (n *Notifier) Notify(ctx context.Context, e domain.Event) error {
	if !notifier.ShouldNotify(n.cfg.NotifyOn, e.State) {
		return nil
	}
	return n.base.Send(ctx, e)
}

func (n *Notifier) url(_ domain.Event) (string, error) {
	return fmt.Sprintf("https://api.%s/api/v2/events", n.cfg.Site), nil
}

func (n *Notifier) auth(req *http.Request) {
	req.Header.Set("DD-API-KEY", n.cfg.APIKey)
}

func (n *Notifier) payload(e domain.Event) (any, error) {
	tags := []string{
		fmt.Sprintf("state:%s", e.State),
		fmt.Sprintf("context:%s", sanitizeTag(e.Context)),
		fmt.Sprintf("namespace:%s", e.Namespace),
		fmt.Sprintf("run_id:%s", e.RunName),
		fmt.Sprintf("resource:%s", e.Resource),
	}
	if e.CommitSHA != "" {
		tags = append(tags, fmt.Sprintf("commit_sha:%s", e.CommitSHA[:min(7, len(e.CommitSHA))]))
	}
	tags = append(tags, n.cfg.Tags...)

	alertType := alertTypeFor(e.State)

	return map[string]any{
		"title":            fmt.Sprintf("[tekton-events-relay] %s — %s", e.Context, e.State),
		"text":             e.Description + "\n\nRun: " + e.Namespace + "/" + e.RunName,
		"alert_type":       alertType,
		"tags":             tags,
		"source_type_name": notifier.UserAgent,
	}, nil
}

func alertTypeFor(s domain.State) string {
	switch s {
	case domain.StateSuccess:
		return alertSuccess
	case domain.StateFailure, domain.StateError:
		return alertError
	default:
		return alertInfo
	}
}

func sanitizeTag(s string) string {
	return strings.NewReplacer("/", "_", ":", "_").Replace(s)
}
