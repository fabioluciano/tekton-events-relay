package azuredevops

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// LabelHandler applies labels to Azure DevOps pull requests.
type LabelHandler struct {
	client       *Client
	successLabel string
	failureLabel string
}

// LabelConfig configures the label handler.
type LabelConfig struct {
	Token              string
	BaseURL            string
	Genre              string
	SuccessLabel       string
	FailureLabel       string
	InsecureSkipVerify bool
	Log                *zap.Logger
}

// NewLabelHandler creates a new Azure DevOps label handler.
func NewLabelHandler(cfg LabelConfig) notifier.ActionHandler {
	return &LabelHandler{
		client:       NewClient(cfg.Token, cfg.BaseURL, cfg.Genre, cfg.InsecureSkipVerify, false, cfg.Log),
		successLabel: cfg.SuccessLabel,
		failureLabel: cfg.FailureLabel,
	}
}

// Name returns the handler name.
func (h *LabelHandler) Name() string { return providerAzure }

// Type returns the action type.
func (h *LabelHandler) Type() notifier.ActionType { return notifier.ActionLabel }

// Handle applies a label to an Azure DevOps PR based on state.
func (h *LabelHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerAzure {
		return nil
	}

	if e.PRNumber == nil || e.Repo.Org == "" || e.Repo.Project == "" || e.Repo.Name == "" {
		return nil
	}

	var label string
	switch e.State {
	case domain.StateSuccess:
		label = h.successLabel
	case domain.StateFailure:
		label = h.failureLabel
	default:
		return nil
	}

	if label == "" {
		return nil
	}

	if err := scm.Validate(providerAzure, "label_name", label); err != nil {
		return err
	}

	gitClient, err := git.NewClient(ctx, h.client.conn)
	if err != nil {
		return err
	}

	prID := *e.PRNumber
	labelRef := core.WebApiCreateTagRequestData{
		Name: &label,
	}

	_, err = gitClient.CreatePullRequestLabel(ctx, git.CreatePullRequestLabelArgs{
		Label:         &labelRef,
		RepositoryId:  &e.Repo.Name,
		PullRequestId: &prID,
		Project:       &e.Repo.Project,
	})

	return err
}
