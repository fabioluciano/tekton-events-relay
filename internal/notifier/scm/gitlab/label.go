package gitlab

import (
	"context"
	"fmt"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// LabelHandler applies labels to GitLab issues and merge requests.
type LabelHandler struct {
	client       *Client
	name         string
	successLabel string
	failureLabel string
}

// LabelConfig configures the label handler.
type LabelConfig struct {
	Token              string
	BaseURL            string
	Name               string
	SuccessLabel       string
	FailureLabel       string
	InsecureSkipVerify bool
}

// NewLabelHandler creates a new GitLab label handler.
func NewLabelHandler(cfg LabelConfig) notifier.ActionHandler {
	return &LabelHandler{
		client:       NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify),
		name:         cfg.Name,
		successLabel: cfg.SuccessLabel,
		failureLabel: cfg.FailureLabel,
	}
}

func (h *LabelHandler) Name() string              { return h.name }
func (h *LabelHandler) Type() notifier.ActionType { return notifier.ActionLabel }

// Handle applies a label to a GitLab issue or merge request based on state.
func (h *LabelHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != h.name {
		return nil
	}

	projectID, err := projectIdentifier(e)
	if err != nil {
		return nil
	}

	var resourceType string
	var resourceID int
	if e.IssueNumber != nil {
		resourceType = "issues"
		resourceID = *e.IssueNumber
	} else if e.PRNumber != nil {
		resourceType = "merge_requests"
		resourceID = *e.PRNumber
	} else {
		return nil
	}

	var label string
	switch e.State {
	case domain.StateSuccess:
		if err := scm.Validate(h.name, "label_name", h.successLabel); err != nil {
			return err
		}
		label = h.successLabel
	case domain.StateFailure:
		if err := scm.Validate(h.name, "label_name", h.failureLabel); err != nil {
			return err
		}
		label = h.failureLabel
	default:
		return nil
	}

	if label == "" {
		return nil
	}

	url := fmt.Sprintf("%s/projects/%s/%s/%d",
		strings.TrimRight(h.client.baseURL, "/"), projectID, resourceType, resourceID)

	payload := map[string]any{"add_labels": label}
	return h.client.Do(ctx, "PUT", url, payload)
}
