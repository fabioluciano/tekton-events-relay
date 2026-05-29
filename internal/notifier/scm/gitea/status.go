package gitea

import (
	"context"
	"fmt"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// StatusReporter implements commit status updates for Gitea.
type StatusReporter struct {
	client *Client
}

// NewStatusReporter creates a new Gitea commit status reporter.
func NewStatusReporter(token, baseURL string, insecureSkipVerify bool) notifier.ActionHandler {
	return &StatusReporter{
		client: NewClient(token, baseURL, insecureSkipVerify),
	}
}

func (r *StatusReporter) Name() string              { return "gitea" }
func (r *StatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts commit status to Gitea.
func (r *StatusReporter) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != "gitea" {
		return nil
	}

	if e.Repo.Owner == "" || e.Repo.Name == "" || e.CommitSHA == "" {
		return nil
	}

	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/statuses/%s",
		strings.TrimRight(r.client.baseURL, "/"),
		e.Repo.Owner, e.Repo.Name, e.CommitSHA)

	if err := scm.Validate("gitea", "status_context", e.Context); err != nil {
		return err
	}
	if err := scm.Validate("gitea", "status_description", e.Description); err != nil {
		return err
	}

	payload := map[string]string{
		"state":       giteaStateMap.Map(e.State, "pending"),
		"context":     e.Context,
		"description": e.Description,
		"target_url":  e.TargetURL,
	}

	return r.client.Do(ctx, "POST", url, payload)
}

var giteaStateMap = scm.StateMap{
	domain.StatePending:  "pending",
	domain.StateRunning:  "pending",
	domain.StateSuccess:  "success",
	domain.StateFailure:  "failure",
	domain.StateError:    "error",
	domain.StateCanceled: "error",
}
