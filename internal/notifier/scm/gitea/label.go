package gitea

import (
	"context"
	"fmt"
	"strings"

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
}

// NewLabelHandler creates a new Gitea label handler.
func NewLabelHandler(cfg LabelConfig) notifier.ActionHandler {
	return &LabelHandler{
		client:       NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify),
		successLabel: cfg.SuccessLabel,
		failureLabel: cfg.FailureLabel,
	}
}

func (h *LabelHandler) Name() string              { return "gitea" }
func (h *LabelHandler) Type() notifier.ActionType { return notifier.ActionLabel }

// Handle applies a label to a Gitea issue or PR based on state.
func (h *LabelHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != "gitea" {
		return nil
	}

	if e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	var issueNumber int
	if e.IssueNumber != nil {
		issueNumber = *e.IssueNumber
	} else if e.PRNumber != nil {
		issueNumber = *e.PRNumber
	} else {
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

	if err := scm.Validate("gitea", "label_name", label); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/issues/%d/labels",
		strings.TrimRight(h.client.baseURL, "/"),
		e.Repo.Owner, e.Repo.Name, issueNumber)

	payload := map[string][]string{"labels": []string{label}}
	return h.client.Do(ctx, "POST", url, payload)
}
