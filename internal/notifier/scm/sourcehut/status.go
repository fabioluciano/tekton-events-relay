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
	client *Client
}

// NewStatusReporter creates a new SourceHut commit status reporter.
func NewStatusReporter(token, baseURL string, insecureSkipVerify bool, log *zap.Logger) notifier.ActionHandler {
	return &StatusReporter{
		client: NewClient(token, baseURL, insecureSkipVerify, false, log),
	}
}

// Name returns the handler name.
func (r *StatusReporter) Name() string { return providerName }

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

	manifest := fmt.Sprintf(`image: alpine
sources:
  - https://git.sr.ht/~%s/%s#%s
tasks:
  - status: |
      echo "%s: %s"
      exit %d
`,
		e.Repo.Owner, e.Repo.Name, e.CommitSHA,
		providerNameStateMap.Map(e.State, "pending"),
		e.Description,
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
