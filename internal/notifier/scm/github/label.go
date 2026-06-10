package github

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// LabelHandler applies labels to GitHub issues and pull requests.
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
}

// NewLabelHandler creates a new GitHub label handler.
func NewLabelHandler(cfg LabelConfig, log *zap.Logger) notifier.ActionHandler {
	return &LabelHandler{
		client:       NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, log, false),
		successLabel: cfg.SuccessLabel,
		failureLabel: cfg.FailureLabel,
	}
}

// Name returns the handler name.
func (h *LabelHandler) Name() string { return providerGitHub }

// Type returns the action type.
func (h *LabelHandler) Type() notifier.ActionType { return notifier.ActionLabel }

// Handle applies a label to a GitHub issue or PR based on state.
func (h *LabelHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerGitHub {
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
		return nil // No issue or PR number - normal for TaskRuns not triggered by issue/PR
	}

	var label string
	switch e.State {
	case domain.StateSuccess:
		if err := scm.Validate(providerGitHub, "label_name", h.successLabel); err != nil {
			return err
		}
		label = h.successLabel
	case domain.StateFailure:
		if err := scm.Validate(providerGitHub, "label_name", h.failureLabel); err != nil {
			return err
		}
		label = h.failureLabel
	default:
		return nil // State not final (pending/running) - normal, will trigger on completion
	}

	if label == "" {
		return nil // No label configured for this state
	}

	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/labels",
		h.client.baseURL, e.Repo.Owner, e.Repo.Name, issueNumber)

	payload := map[string][]string{"labels": {label}}
	return h.client.Do(ctx, "POST", url, payload)
}
