package gitlab

import (
	"context"
	"fmt"

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

// ensureLabelsExist creates missing labels with specified colors before applying to issues/MRs.
// GitLab auto-creates labels without color control, so we pre-create with color if specified.
func (h *LabelHandler) ensureLabelsExist(ctx context.Context, projectID string, labels []scm.Label) error {
	if len(labels) == 0 {
		return nil
	}

	// List existing labels
	existingLabels, _, err := h.client.gl.Labels.ListLabels(projectID, &gl.ListLabelsOptions{}, gl.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("list labels: %w", err)
	}

	existing := make(map[string]bool)
	for _, l := range existingLabels {
		existing[l.Name] = true
	}

	// Create missing labels with color
	for _, label := range labels {
		if existing[label.Name] {
			continue // label exists, preserve existing color (idempotent)
		}

		opts := &gl.CreateLabelOptions{
			Name: gl.Ptr(label.Name),
		}
		if label.Color != "" {
			// GitLab expects # prefix
			opts.Color = gl.Ptr("#" + label.Color)
		}

		if _, _, err := h.client.gl.Labels.CreateLabel(projectID, opts, gl.WithContext(ctx)); err != nil {
			return fmt.Errorf("create label %q: %w", label.Name, err)
		}
	}

	return nil
}

// applyLabelSet executes the declarative add/remove effect in a single
// atomic update call; creates missing labels with color before applying.
func (h *LabelHandler) applyLabelSet(ctx context.Context, e domain.Event, projectID string) error {
	add, remove, err := h.labels.Render(e)
	if err != nil {
		return err
	}

	// Validate all label names
	for _, label := range append(append([]scm.Label{}, add...), remove...) {
		if err := scm.Validate(h.name, "label_name", label.Name); err != nil {
			return err
		}
	}

	// Ensure labels with colors exist at project level
	if err := h.ensureLabelsExist(ctx, projectID, add); err != nil {
		return err
	}

	// Convert to label names for GitLab SDK
	var addOpts, removeOpts *gl.LabelOptions
	if len(add) > 0 {
		addNames := make([]string, len(add))
		for i, label := range add {
			addNames[i] = label.Name
		}
		v := gl.LabelOptions(addNames)
		addOpts = &v
	}
	if len(remove) > 0 {
		removeNames := make([]string, len(remove))
		for i, label := range remove {
			removeNames[i] = label.Name
		}
		v := gl.LabelOptions(removeNames)
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
