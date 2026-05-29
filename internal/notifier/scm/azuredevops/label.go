package azuredevops

import (
	"context"
	"fmt"
	"strings"

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
}

// NewLabelHandler creates a new Azure DevOps label handler.
func NewLabelHandler(cfg LabelConfig) notifier.ActionHandler {
	return &LabelHandler{
		client:       NewClient(cfg.Token, cfg.BaseURL, cfg.Genre, cfg.InsecureSkipVerify),
		successLabel: cfg.SuccessLabel,
		failureLabel: cfg.FailureLabel,
	}
}

func (h *LabelHandler) Name() string              { return "azure-devops" }
func (h *LabelHandler) Type() notifier.ActionType { return notifier.ActionLabel }

// Handle applies a label to an Azure DevOps PR based on state.
func (h *LabelHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != "azure-devops" {
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

	if err := scm.Validate("azure-devops", "label_name", label); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d/labels?api-version=7.1",
		strings.TrimRight(h.client.baseURL, "/"),
		e.Repo.Org, e.Repo.Project, e.Repo.Name, *e.PRNumber)

	payload := map[string]string{"name": label}

	return h.client.Do(ctx, "POST", url, payload)
}
