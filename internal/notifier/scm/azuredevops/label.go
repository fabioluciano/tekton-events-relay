package azuredevops

import (
	"context"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// LabelHandler applies labels to Azure DevOps pull requests.
type LabelHandler struct {
	client *Client
	labels scm.LabelSet
	log    *zap.Logger
}

// LabelConfig configures the label handler.
type LabelConfig struct {
	Token              string
	BaseURL            string
	Genre              string
	Labels             scm.LabelSet
	InsecureSkipVerify bool
	Log                *zap.Logger
}

// NewLabelHandler creates a new Azure DevOps label handler.
func NewLabelHandler(cfg LabelConfig) notifier.ActionHandler {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &LabelHandler{
		client: NewClient(cfg.Token, cfg.BaseURL, cfg.Genre, cfg.InsecureSkipVerify, false, cfg.Log),
		labels: cfg.Labels,
		log:    log,
	}
}

// Name returns the handler name.
func (h *LabelHandler) Name() string { return providerAzure }

// Provider returns the provider type identifier.
func (h *LabelHandler) Provider() string { return providerAzure }

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

	if h.labels.Empty() {
		return nil // nothing declared — config validation rejects this upfront
	}
	return h.applyLabelSet(ctx, e)
}

// applyLabelSet executes the declarative add/remove effect on the PR.
// Removing an absent label is treated as success.
func (h *LabelHandler) applyLabelSet(ctx context.Context, e domain.Event) error {
	add, remove, err := h.labels.Render(e)
	if err != nil {
		return err
	}

	gitClient, err := git.NewClient(ctx, h.client.conn)
	if err != nil {
		return err
	}
	prID := *e.PRNumber

	for _, label := range remove {
		if err := scm.Validate(providerAzure, "label_name", label.Name); err != nil {
			return err
		}
		labelName := label.Name
		if err := gitClient.DeletePullRequestLabels(ctx, git.DeletePullRequestLabelsArgs{
			RepositoryId:  &e.Repo.Name,
			PullRequestId: &prID,
			Project:       &e.Repo.Project,
			LabelIdOrName: &labelName,
		}); err != nil {
			h.log.Warn("azure label removal failed (label may be absent)",
				zap.String("label", label.Name), zap.Error(err))
		}
	}

	// Azure DevOps tags don't support colors; color field is ignored
	for _, label := range add {
		if err := scm.Validate(providerAzure, "label_name", label.Name); err != nil {
			return err
		}
		labelName := label.Name
		if _, err := gitClient.CreatePullRequestLabel(ctx, git.CreatePullRequestLabelArgs{
			Label:         &core.WebApiCreateTagRequestData{Name: &labelName},
			RepositoryId:  &e.Repo.Name,
			PullRequestId: &prID,
			Project:       &e.Repo.Project,
		}); err != nil {
			return fmt.Errorf("add label %q: %w", label.Name, err)
		}
	}
	return nil
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *LabelHandler) Close() error { return nil }
