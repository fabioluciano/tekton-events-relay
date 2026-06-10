package gitea

import (
	"context"
	"fmt"

	giteaSDK "code.gitea.io/sdk/gitea"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// LabelHandler applies labels to Gitea issues and pull requests.
type LabelHandler struct {
	client       *Client
	successLabel string
	failureLabel string
}

// LabelConfig configures the label handler.
type LabelConfig struct {
	Token              string
	BaseURL            string
	SuccessLabel       string
	FailureLabel       string
	InsecureSkipVerify bool
	Log                *zap.Logger
}

// NewLabelHandler creates a new Gitea label handler.
func NewLabelHandler(cfg LabelConfig) notifier.ActionHandler {
	return &LabelHandler{
		client:       NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, false, cfg.Log),
		successLabel: cfg.SuccessLabel,
		failureLabel: cfg.FailureLabel,
	}
}

// Name returns the handler name.
func (h *LabelHandler) Name() string { return providerGitea }

// Type returns the action type.
func (h *LabelHandler) Type() notifier.ActionType { return notifier.ActionLabel }

// Handle applies a label to a Gitea issue or PR based on state.
func (h *LabelHandler) Handle(_ context.Context, e domain.Event) error {
	if e.Provider != providerGitea {
		return nil
	}

	if e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	var issueNumber int
	switch {
	case e.IssueNumber != nil:
		issueNumber = *e.IssueNumber
	case e.PRNumber != nil:
		issueNumber = *e.PRNumber
	default:
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

	if err := scm.Validate(providerGitea, "label_name", label); err != nil {
		return err
	}

	// First, list all repo labels to find the ID for the label name
	labels, _, err := h.client.sdk.ListRepoLabels(e.Repo.Owner, e.Repo.Name, giteaSDK.ListLabelsOptions{})
	if err != nil {
		return fmt.Errorf("list repo labels: %w", err)
	}

	var labelID int64
	for _, l := range labels {
		if l.Name == label {
			labelID = l.ID
			break
		}
	}

	if labelID == 0 {
		return fmt.Errorf("label %q not found in repository", label)
	}

	// Add the label by ID
	_, _, err = h.client.sdk.AddIssueLabels(e.Repo.Owner, e.Repo.Name, int64(issueNumber), giteaSDK.IssueLabelsOption{
		Labels: []int64{labelID},
	})
	return err
}
