// Package incidentio implements the Notifier for Incident.io via the v2 Incidents API.
// Doc: https://docs.incident.io/api-reference/incidents-v2/create
//
// Behavior:
//   - StateFailure / StateError → creates an incident
//   - other states              → ignored (no resolve/close endpoint; incidents managed manually)
//
// Idempotency uses RunID as idempotency_key — ensures that
// multiple events from the same run don't create duplicate incidents.
package incidentio

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
	createIncidentsURL = "https://api.incident.io/v2/incidents"
)

// Config contains Incident.io integration settings.
type Config struct {
	Name string
	// APIKey provides the Incident.io API key, resolved fresh per request
	// so rotated secrets are picked up without a pod restart.
	APIKey scm.TokenRefresher
	// SeverityID is the Incident.io severity ID to assign (optional).
	SeverityID string
	// IncidentTypeID is the Incident.io incident type ID to assign (optional).
	IncidentTypeID string
	// Visibility controls whether the incident is public or private. Default: public.
	Visibility string
	// HTTPClient overrides the HTTP client. When nil, notifier.DefaultHTTPClient() is used.
	HTTPClient *http.Client
	// RetryPolicy overrides the global retry policy. When nil, the global default is used.
	RetryPolicy *httpx.RetryPolicy
}

// Notifier sends failure events to Incident.io as incidents.
type Notifier struct {
	name string
	base *notifier.Base
	cfg  Config
}

// New creates an Incident.io notifier.
func New(cfg Config, log *zap.Logger) *Notifier {
	if cfg.Visibility == "" {
		cfg.Visibility = "public"
	}
	n := &Notifier{name: cfg.Name, cfg: cfg}
	httpClient := notifier.DefaultHTTPClient()
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}
	n.base = &notifier.Base{
		HTTP:         httpClient,
		BuildURL:     func(_ domain.Event) (string, error) { return createIncidentsURL, nil },
		BuildPayload: n.payload,
		Auth:         n.auth,
		UserAgent:    notifier.UserAgent,
		Log:          log,
		RetryPolicy:  cfg.RetryPolicy,
	}
	return n
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return n.name }

// Type returns the action type for generic notifiers.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle creates an Incident.io incident on failure/error states.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	if e.State != domain.StateFailure && e.State != domain.StateError {
		return nil // only create incidents on failure
	}
	return n.base.Send(ctx, e)
}

func (n *Notifier) auth(req *http.Request) error {
	if n.cfg.APIKey == nil {
		return fmt.Errorf("incidentio: api key refresher is required")
	}
	apiKey, err := n.cfg.APIKey.Token(req.Context())
	if err != nil {
		return fmt.Errorf("incidentio: resolve api key: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return nil
}

func (n *Notifier) payload(e domain.Event) (any, error) {
	p := map[string]any{
		"name":            fmt.Sprintf("Pipeline %s failed", e.RunName),
		"idempotency_key": fmt.Sprintf("tekton-relay:%s", e.RunID),
		"visibility":      n.cfg.Visibility,
		"mode":            "standard",
		"summary":         fmt.Sprintf("Tekton pipeline %s/%s failed in namespace %s", e.PipelineName, e.RunName, e.Namespace),
	}

	if n.cfg.SeverityID != "" {
		p["severity_id"] = n.cfg.SeverityID
	}
	if n.cfg.IncidentTypeID != "" {
		p["incident_type_id"] = n.cfg.IncidentTypeID
	}

	return p, nil
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (n *Notifier) Close() error { return nil }
