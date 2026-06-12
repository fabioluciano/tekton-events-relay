package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// LabelHandler applies labels to GitHub issues and pull requests.
type LabelHandler struct {
	client *Client
	labels scm.LabelSet
	log    *zap.Logger
}

// LabelConfig configures the label handler.
type LabelConfig struct {
	Token              string
	BaseURL            string
	Labels             scm.LabelSet
	InsecureSkipVerify bool
}

// NewLabelHandler creates a new GitHub label handler.
func NewLabelHandler(cfg LabelConfig, log *zap.Logger) notifier.ActionHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &LabelHandler{
		client: NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, log, false),
		labels: cfg.Labels,
		log:    log,
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

	if h.labels.Empty() {
		return nil // nothing declared — config validation rejects this upfront
	}
	return h.applyLabelSet(ctx, e, issueNumber)
}

// applyLabelSet executes the declarative add/remove effect: removals run
// first so overlapping names converge, and removing an absent label (404)
// is treated as success.
func (h *LabelHandler) applyLabelSet(ctx context.Context, e domain.Event, issueNumber int) error {
	add, remove, err := h.labels.Render(e)
	if err != nil {
		return err
	}

	base := fmt.Sprintf("%s/repos/%s/%s/issues/%d/labels",
		h.client.baseURL, e.Repo.Owner, e.Repo.Name, issueNumber)

	for _, name := range remove {
		if err := scm.Validate(providerGitHub, "label_name", name); err != nil {
			return err
		}
		if err := h.client.Do(ctx, "DELETE", base+"/"+url.PathEscape(name), nil); err != nil {
			if strings.Contains(err.Error(), "returned 404") {
				continue // label not present: removal already satisfied
			}
			return err
		}
	}

	if len(add) == 0 {
		return nil
	}
	for _, name := range add {
		if err := scm.Validate(providerGitHub, "label_name", name); err != nil {
			return err
		}
	}
	// GitHub creates missing labels automatically on this endpoint.
	return h.client.Do(ctx, "POST", base, map[string][]string{"labels": add})
}
