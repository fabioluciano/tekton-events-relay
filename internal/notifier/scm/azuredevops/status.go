package azuredevops

import (
	"context"
	"fmt"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// StatusReporter implements commit status updates for Azure DevOps.
type StatusReporter struct {
	client *Client
}

// NewStatusReporter creates a new Azure DevOps commit status reporter.
func NewStatusReporter(token, baseURL, genre string, insecureSkipVerify bool) notifier.ActionHandler {
	return &StatusReporter{
		client: NewClient(token, baseURL, genre, insecureSkipVerify),
	}
}

func (r *StatusReporter) Name() string              { return "azure-devops" }
func (r *StatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts commit status to Azure DevOps.
func (r *StatusReporter) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != "azure-devops" {
		return nil
	}

	if e.Repo.Org == "" || e.Repo.Project == "" || e.Repo.Name == "" || e.CommitSHA == "" {
		return nil
	}

	url := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/commits/%s/statuses?api-version=7.1",
		strings.TrimRight(r.client.baseURL, "/"),
		e.Repo.Org, e.Repo.Project, e.Repo.Name, e.CommitSHA)

	if err := scm.Validate("azure-devops", "status_description", e.Description); err != nil {
		return err
	}

	payload := AzureStatus{
		State:       azureStateMap.Map(e.State, "pending"),
		Description: e.Description,
		TargetURL:   e.TargetURL,
		Context: AzureContext{
			Name:  e.Context,
			Genre: r.client.genre,
		},
	}

	return r.client.Do(ctx, "POST", url, payload)
}

var azureStateMap = scm.StateMap{
	domain.StatePending:  "pending",
	domain.StateRunning:  "pending",
	domain.StateSuccess:  "succeeded",
	domain.StateFailure:  "failed",
	domain.StateError:    "error",
	domain.StateCanceled: "error",
}
