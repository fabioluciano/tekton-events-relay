// Package sourcehut implements the Reporter for SourceHut (sr.ht).
//
// SourceHut doesn't have a "commit status API" REST like others. The equivalent
// is to create a build job in builds.sr.ht via GraphQL API. To reflect the
// status of an external execution (Tekton), the supported approach is to submit
// a minimal job with the desired status, OR use the builds REST API to
// query/update existing jobs.
//
// This Reporter uses the builds.sr.ht REST API to submit a status job.
// For production flows, consider integrating via builds.sr.ht webhooks
// in the reverse direction.
//
// Doc: https://man.sr.ht/builds.sr.ht/api.md
package sourcehut

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// Config holds the configuration for the SourceHut reporter.
type Config struct {
	Token              string // OAuth token sr.ht
	BaseURL            string // default https://builds.sr.ht
	InsecureSkipVerify bool   // Skip TLS certificate verification
}

// Reporter implements the notifier for SourceHut builds API.
type Reporter struct {
	base *notifier.Base
	cfg  Config
}

// New creates a new SourceHut reporter with the given configuration.
func New(cfg Config) *Reporter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://builds.sr.ht"
	}
	r := &Reporter{cfg: cfg}
	r.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildPayload: r.payload,
		BuildURL:     r.url,
		Auth:         r.auth,
		UserAgent:    notifier.UserAgent,
	}
	return r
}

// Name returns the identifier for this notifier.
func (r *Reporter) Name() string { return "sourcehut" }

// Notify sends a build job to SourceHut.
func (r *Reporter) Notify(ctx context.Context, s domain.Event) error {
	return r.base.Send(ctx, s)
}

// sr.ht builds states: pending, queued, running, success, failed, timeout, cancelled
var sourcehutStateMapLegacy = scm.StateMap{
	domain.StatePending:  "pending",
	domain.StateRunning:  "running",
	domain.StateSuccess:  "success",
	domain.StateFailure:  "failed",
	domain.StateError:    "failed",
	domain.StateCanceled: "cancelled",
}

func (r *Reporter) url(s domain.Event) (string, error) {
	base := s.APIBaseURL
	if base == "" {
		base = r.cfg.BaseURL
	}
	// Submit a minimal job. In production, this endpoint should be adjusted
	// according to the specific integration (manifests, secrets, etc).
	return strings.TrimRight(base, "/") + "/api/jobs", nil
}

func (r *Reporter) payload(s domain.Event) (any, error) {
	if s.Repo.Owner == "" || s.Repo.Name == "" {
		return nil, fmt.Errorf("sourcehut requires owner and name")
	}
	// Minimal YAML manifest that reflects the status. For serious integration,
	// replace with GraphQL call to `createJob` mutation.
	manifest := fmt.Sprintf(`image: alpine
sources:
  - https://git.sr.ht/~%s/%s#%s
tasks:
  - status: |
      echo "%s: %s"
      exit %d
`,
		s.Repo.Owner, s.Repo.Name, s.CommitSHA,
		sourcehutStateMapLegacy.Map(s.State, "pending"), s.Description, exitFor(s.State),
	)

	return map[string]any{
		"manifest": manifest,
		"note":     s.Description,
		"tags":     []string{"tekton", s.Context},
	}, nil
}

func (r *Reporter) auth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+r.cfg.Token)
}

func exitFor(s domain.State) int {
	if s == domain.StateFailure || s == domain.StateError {
		return 1
	}
	return 0
}
