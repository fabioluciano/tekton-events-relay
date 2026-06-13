package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"

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
	client *Client
	labels scm.LabelSet
	log    *zap.Logger
	cache  *labelCache
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
	labels := cfg.Labels
	labels.Validate(log)
	return &LabelHandler{
		client: NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, log, false),
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
	labelURL := fmt.Sprintf("%s/repos/%s/%s/labels/%s",
		h.client.baseURL, owner, repo, url.PathEscape(label.Name))
	err := h.client.Do(ctx, "GET", labelURL, nil)
	if err == nil {
		// Label exists, cache it and preserve existing color (idempotent)
		h.cache.mu.Lock()
		h.cache.checked[cacheKey] = true
		h.cache.mu.Unlock()
		return nil
	}

	// If not 404, propagate error
	if !strings.Contains(err.Error(), "returned 404") {
		return fmt.Errorf("check label existence: %w", err)
	}

	// Label missing: create with color
	createURL := fmt.Sprintf("%s/repos/%s/%s/labels", h.client.baseURL, owner, repo)
	payload := map[string]string{
		"name":  label.Name,
		"color": label.Color,
	}
	if err := h.client.Do(ctx, "POST", createURL, payload); err != nil {
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

	base := fmt.Sprintf("%s/repos/%s/%s/issues/%d/labels",
		h.client.baseURL, e.Repo.Owner, e.Repo.Name, issueNumber)

	for _, label := range remove {
		if err := scm.Validate(providerGitHub, "label_name", label.Name); err != nil {
			return err
		}
		if err := h.client.Do(ctx, "DELETE", base+"/"+url.PathEscape(label.Name), nil); err != nil {
			if strings.Contains(err.Error(), "returned 404") {
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
	return h.client.Do(ctx, "POST", base, map[string][]string{"labels": labelNames})
}
