package sourcehut

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// StatusReporter implements commit status updates for SourceHut via builds API.
type StatusReporter struct {
	name   string
	client *Client
}

// NewStatusReporter creates a new SourceHut commit status reporter.
func NewStatusReporter(name, token, baseURL string, insecureSkipVerify bool, log *zap.Logger) notifier.ActionHandler {
	return &StatusReporter{
		name:   name,
		client: NewClient(token, baseURL, insecureSkipVerify, false, log),
	}
}

// Name returns the handler name.
func (r *StatusReporter) Name() string { return r.name }

// Provider returns the provider type identifier.
func (r *StatusReporter) Provider() string { return providerName }

// Type returns the action type.
func (r *StatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts a minimal build job to SourceHut representing the commit status.
func (r *StatusReporter) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerName {
		return nil
	}

	if e.Repo.Owner == "" || e.Repo.Name == "" || e.CommitSHA == "" {
		return nil
	}

	url := strings.TrimRight(r.client.BaseURL, "/") + "/api/jobs"

	if err := scm.Validate(providerName, "status_description", e.Description); err != nil {
		return err
	}

	safeOwner := sanitizeYAML(e.Repo.Owner)
	safeRepo := sanitizeYAML(e.Repo.Name)
	safeSHA := sanitizeYAML(e.CommitSHA)
	safeDesc := sanitizeYAML(e.Description)

	manifest := fmt.Sprintf(`image: alpine
sources:
  - https://git.sr.ht/~%s/%s#%s
tasks:
  - status: |
      echo "%s: %s"
      exit %d
`,
		safeOwner, safeRepo, safeSHA,
		providerNameStateMap.Map(e.State, "pending"),
		safeDesc,
		exitFor(e.State),
	)

	payload := map[string]any{
		"manifest": manifest,
		"note":     e.Description,
		"tags":     []string{"tekton", e.Context},
	}

	return r.client.Do(ctx, "POST", url, payload)
}

var providerNameStateMap = scm.StateMap{
	domain.StatePending:  "pending",
	domain.StateRunning:  "running",
	domain.StateSuccess:  "success",
	domain.StateFailure:  "failed",
	domain.StateError:    "failed",
	domain.StateCanceled: "cancelled",
}

// sanitizeYAML removes characters that could break YAML block scalars.
func sanitizeYAML(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\"", "'")
	return s
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (r *StatusReporter) Close() error { return nil }
