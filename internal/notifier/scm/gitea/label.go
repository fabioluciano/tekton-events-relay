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
	Log                *zap.Logger
}

// NewLabelHandler creates a new Gitea label handler.
func NewLabelHandler(cfg LabelConfig) notifier.ActionHandler {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &LabelHandler{
		client: NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, false, cfg.Log),
		labels: cfg.Labels,
		log:    log,
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

	if h.labels.Empty() {
		return nil // nothing declared — config validation rejects this upfront
	}
	return h.applyLabelSet(e, int64(issueNumber))
}

// applyLabelSet executes the declarative add/remove effect. Gitea labels
// are referenced by ID: missing labels are created on add (default color)
// and silently skipped on remove.
func (h *LabelHandler) applyLabelSet(e domain.Event, issueNumber int64) error {
	add, remove, err := h.labels.Render(e)
	if err != nil {
		return err
	}

	repoLabels, _, err := h.client.sdk.ListRepoLabels(e.Repo.Owner, e.Repo.Name, giteaSDK.ListLabelsOptions{})
	if err != nil {
		return fmt.Errorf("list repo labels: %w", err)
	}
	byName := make(map[string]int64, len(repoLabels))
	for _, l := range repoLabels {
		byName[l.Name] = l.ID
	}

	for _, label := range remove {
		if err := scm.Validate(providerGitea, "label_name", label.Name); err != nil {
			return err
		}
		id, ok := byName[label.Name]
		if !ok {
			continue // label absent: removal already satisfied
		}
		if _, err := h.client.sdk.DeleteIssueLabel(e.Repo.Owner, e.Repo.Name, issueNumber, id); err != nil {
			h.log.Warn("gitea label removal failed", zap.String("label", label.Name), zap.Error(err))
		}
	}

	ids := make([]int64, 0, len(add))
	for _, label := range add {
		if err := scm.Validate(providerGitea, "label_name", label.Name); err != nil {
			return err
		}
		id, ok := byName[label.Name]
		if !ok {
			// Use custom color or default to gray
			color := label.Color
			if color == "" {
				color = "ededed" // Gitea default gray
			}
			created, _, err := h.client.sdk.CreateLabel(e.Repo.Owner, e.Repo.Name, giteaSDK.CreateLabelOption{
				Name:  label.Name,
				Color: color,
			})
			if err != nil {
				return fmt.Errorf("create label %q: %w", label.Name, err)
			}
			id = created.ID
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	_, _, err = h.client.sdk.AddIssueLabels(e.Repo.Owner, e.Repo.Name, issueNumber, giteaSDK.IssueLabelsOption{Labels: ids})
	return err
}
