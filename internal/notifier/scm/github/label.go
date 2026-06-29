package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	gh "github.com/google/go-github/v68/github"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

type labelCache struct {
	mu      sync.RWMutex
	checked map[string]bool // key: "owner/repo/labelname"
}

// LabelHandler applies labels to GitHub issues and pull requests.
type LabelHandler struct {
	client HTTPDoer
	labels scm.LabelSet
	log    *zap.Logger
	cache  *labelCache
}

// LabelConfig configures the label handler.
type LabelConfig struct {
	Client HTTPDoer
	Labels scm.LabelSet
}

// NewLabelHandler creates a new GitHub label handler.
func NewLabelHandler(cfg LabelConfig, log *zap.Logger) notifier.ActionHandler {
	if log == nil {
		log = zap.NewNop()
	}
	labels := cfg.Labels
	labels.Validate(log)
	return &LabelHandler{
		client: cfg.Client,
		labels: labels,
		log:    log,
		cache: &labelCache{
			checked: make(map[string]bool),
		},
	}
}

// Name returns the handler name.
func (h *LabelHandler) Name() string { return providerGitHub }

// Type returns the action type.
func (h *LabelHandler) Type() notifier.ActionType { return notifier.ActionLabel }

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *LabelHandler) Close() error { return nil }

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

// ensureLabelExists checks if a label exists in the repository and creates it
// with the specified color if missing. Uses cache to avoid redundant API calls.
func (h *LabelHandler) ensureLabelExists(ctx context.Context, owner, repo string, label scm.Label) error {
	if label.Color == "" {
		return nil // empty color: let issue-scoped endpoint create with random color
	}

	cacheKey := owner + "/" + repo + "/" + label.Name

	// Check cache first
	h.cache.mu.RLock()
	exists := h.cache.checked[cacheKey]
	h.cache.mu.RUnlock()
	if exists {
		return nil
	}

	// Check if label exists via repo-scoped GET
	existingLabel, _, err := h.client.GH().Issues.GetLabel(ctx, owner, repo, label.Name)
	if err == nil {
		// Label exists - update color if different
		if existingLabel.GetColor() != label.Color {
			if _, _, err := h.client.GH().Issues.EditLabel(ctx, owner, repo, label.Name, &gh.Label{Color: gh.Ptr(label.Color)}); err != nil {
				return fmt.Errorf("update label color: %w", err)
			}
		}
		// Cache it
		h.cache.mu.Lock()
		h.cache.checked[cacheKey] = true
		h.cache.mu.Unlock()
		return nil
	}

	// If not 404, propagate error
	var ghErr *gh.ErrorResponse
	if !errors.As(err, &ghErr) || ghErr.Response == nil || ghErr.Response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("check label existence: %w", err)
	}

	// Label missing: create with color
	if _, _, err := h.client.GH().Issues.CreateLabel(ctx, owner, repo, &gh.Label{
		Name:  gh.Ptr(label.Name),
		Color: gh.Ptr(label.Color),
	}); err != nil {
		return fmt.Errorf("create label with color: %w", err)
	}

	// Cache successful creation
	h.cache.mu.Lock()
	h.cache.checked[cacheKey] = true
	h.cache.mu.Unlock()
	return nil
}

// applyLabelSet executes the declarative add/remove effect: removals run
// first so overlapping names converge, and removing an absent label (404)
// is treated as success.
func (h *LabelHandler) applyLabelSet(ctx context.Context, e domain.Event, issueNumber int) error {
	add, remove, err := h.labels.Render(e)
	if err != nil {
		return err
	}

	for _, label := range remove {
		if err := scm.Validate(providerGitHub, "label_name", label.Name); err != nil {
			return err
		}
		_, err := h.client.GH().Issues.RemoveLabelForIssue(ctx, e.Repo.Owner, e.Repo.Name, issueNumber, label.Name)
		if err != nil {
			var ghErr *gh.ErrorResponse
			if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotFound {
				continue // label not present: removal already satisfied
			}
			return err
		}
	}

	if len(add) == 0 {
		return nil
	}

	// Ensure labels with colors exist at repo level before applying to issue
	for _, label := range add {
		if err := scm.Validate(providerGitHub, "label_name", label.Name); err != nil {
			return err
		}
		if err := h.ensureLabelExists(ctx, e.Repo.Owner, e.Repo.Name, label); err != nil {
			return err
		}
	}

	// Apply labels to issue (GitHub creates missing labels automatically if no color specified)
	labelNames := make([]string, len(add))
	for i, label := range add {
		labelNames[i] = label.Name
	}
	_, _, err = h.client.GH().Issues.AddLabelsToIssue(ctx, e.Repo.Owner, e.Repo.Name, issueNumber, labelNames)
	return err
}
