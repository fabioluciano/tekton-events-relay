package gitlab

import (
	"context"

	gl "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// LabelHandler applies labels to GitLab issues and merge requests.
type LabelHandler struct {
	client *Client
	name   string
	labels scm.LabelSet
}

// LabelConfig configures the label handler.
type LabelConfig struct {
	Token              string
	BaseURL            string
	Name               string
	Labels             scm.LabelSet
	InsecureSkipVerify bool
	Log                *zap.Logger
}

// NewLabelHandler creates a new GitLab label handler.
func NewLabelHandler(cfg LabelConfig) notifier.ActionHandler {
	return &LabelHandler{
		client: NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, false, cfg.Log),
		name:   cfg.Name,
		labels: cfg.Labels,
	}
}

// Name returns the handler name.
func (h *LabelHandler) Name() string { return h.name }

// Type returns the action type.
func (h *LabelHandler) Type() notifier.ActionType { return notifier.ActionLabel }

// Handle applies a label to a GitLab issue or merge request based on state.
func (h *LabelHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != h.name {
		return nil
	}

	projectID, pErr := projectIdentifier(e)
	if pErr != nil {
		return nil //nolint:nilerr // intentional: drop event if project cannot be identified
	}

	if h.labels.Empty() {
		return nil // nothing declared — config validation rejects this upfront
	}
	return h.applyLabelSet(ctx, e, projectID)
}

// applyLabelSet executes the declarative add/remove effect in a single
// atomic update call; GitLab creates missing labels automatically and
// ignores removals of absent labels.
func (h *LabelHandler) applyLabelSet(ctx context.Context, e domain.Event, projectID string) error {
	add, remove, err := h.labels.Render(e)
	if err != nil {
		return err
	}
	for _, name := range append(append([]string{}, add...), remove...) {
		if err := scm.Validate(h.name, "label_name", name); err != nil {
			return err
		}
	}

	var addOpts, removeOpts *gl.LabelOptions
	if len(add) > 0 {
		v := gl.LabelOptions(add)
		addOpts = &v
	}
	if len(remove) > 0 {
		v := gl.LabelOptions(remove)
		removeOpts = &v
	}

	switch {
	case e.IssueNumber != nil:
		opts := &gl.UpdateIssueOptions{AddLabels: addOpts, RemoveLabels: removeOpts}
		_, _, err := h.client.gl.Issues.UpdateIssue(projectID, int64(*e.IssueNumber), opts, gl.WithContext(ctx))
		return err
	case e.PRNumber != nil:
		opts := &gl.UpdateMergeRequestOptions{AddLabels: addOpts, RemoveLabels: removeOpts}
		_, _, err := h.client.gl.MergeRequests.UpdateMergeRequest(projectID, int64(*e.PRNumber), opts, gl.WithContext(ctx))
		return err
	default:
		return nil
	}
}
