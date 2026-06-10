package azuredevops

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// StatusReporter implements commit status updates for Azure DevOps.
type StatusReporter struct {
	client *Client
}

// NewStatusReporter creates a new Azure DevOps commit status reporter.
func NewStatusReporter(token, baseURL, genre string, insecureSkipVerify bool, log *zap.Logger) notifier.ActionHandler {
	return &StatusReporter{
		client: NewClient(token, baseURL, genre, insecureSkipVerify, false, log),
	}
}

// Name returns the handler name.
func (r *StatusReporter) Name() string { return providerAzure }

// Type returns the action type.
func (r *StatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts commit status to Azure DevOps.
func (r *StatusReporter) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerAzure {
		return nil
	}

	if e.Repo.Org == "" || e.Repo.Project == "" || e.Repo.Name == "" || e.CommitSHA == "" {
		return nil
	}

	if err := scm.Validate(providerAzure, "status_description", e.Description); err != nil {
		return err
	}

	gitClient, err := git.NewClient(ctx, r.client.conn)
	if err != nil {
		return err
	}

	state := git.GitStatusState(azureStateMap.Map(e.State, "pending"))
	status := git.GitStatus{
		State:       &state,
		Description: &e.Description,
		TargetUrl:   &e.TargetURL,
		Context: &git.GitStatusContext{
			Name:  &e.Context,
			Genre: &r.client.genre,
		},
	}

	_, err = gitClient.CreateCommitStatus(ctx, git.CreateCommitStatusArgs{
		GitCommitStatusToCreate: &status,
		CommitId:                &e.CommitSHA,
		RepositoryId:            &e.Repo.Name,
		Project:                 &e.Repo.Project,
	})

	return err
}

var azureStateMap = scm.StateMap{
	domain.StatePending:  "pending",
	domain.StateRunning:  "pending",
	domain.StateSuccess:  "succeeded",
	domain.StateFailure:  "failed",
	domain.StateError:    "error",
	domain.StateCanceled: "error",
}
