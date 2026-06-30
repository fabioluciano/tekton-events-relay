package jira

import (
	"context"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// LinkCommitHandler posts a remote link on a Jira issue pointing to the commit.
// Only fires when CommitSHA is non-empty.
type LinkCommitHandler struct {
	client   *Client
	issueKey string
	log      *zap.Logger
}

// NewLinkCommitHandler builds a link_commit handler.
func NewLinkCommitHandler(client *Client, issueKey string, log *zap.Logger) (*LinkCommitHandler, error) {
	if issueKey == "" {
		return nil, fmt.Errorf("jira: link_commit issue_key is required")
	}
	return &LinkCommitHandler{client: client, issueKey: issueKey, log: log}, nil
}

// Name returns the provider identifier.
func (h *LinkCommitHandler) Name() string { return notifierName }

// Provider returns the provider type identifier.
func (h *LinkCommitHandler) Provider() string { return notifierName }

// Type returns the action type.
func (h *LinkCommitHandler) Type() notifier.ActionType { return notifier.ActionJiraLinkCommit }

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *LinkCommitHandler) Close() error { return nil }

// Handle posts a remote link on the configured Jira issue. Events without a
// CommitSHA are skipped (nothing to link).
func (h *LinkCommitHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.CommitSHA == "" {
		h.log.Debug("jira link_commit skipped: no commit SHA",
			zap.String("run", e.RunName))
		return nil
	}

	title := fmt.Sprintf("Commit %s", e.CommitSHA[:minLen(8, len(e.CommitSHA))])
	if e.PipelineName != "" {
		title = fmt.Sprintf("%s — %s", e.PipelineName, title)
	}

	commitURL := e.TargetURL
	if commitURL == "" {
		commitURL = fmt.Sprintf("%s/commit/%s", e.APIBaseURL, e.CommitSHA)
	}

	payload := map[string]any{
		"globalId": fmt.Sprintf("tekton-relay:%s:%s", e.RunID, e.CommitSHA),
		"object": map[string]any{
			"url":   commitURL,
			"title": title,
			"icon": map[string]string{
				"url16x16": "https://tekton.dev/favicon.png",
				"title":    "Tekton Pipeline",
			},
		},
		"status": map[string]string{
			"resolved": string(e.State),
		},
	}

	url := fmt.Sprintf("%s/rest/api/3/issue/%s/remotelink", h.client.BaseURL, h.issueKey)
	if err := h.client.Do(ctx, http.MethodPost, url, payload); err != nil {
		return fmt.Errorf("jira: link commit %s to %s: %w", e.CommitSHA, h.issueKey, err)
	}

	h.log.Debug("jira remote link created",
		zap.String("issue", h.issueKey),
		zap.String("commit", e.CommitSHA),
		zap.String("run", e.RunName))
	return nil
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
