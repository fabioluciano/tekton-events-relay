package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const dedupeLabelPrefix = "tekton-relay:"

// CreateIssueHandler creates a Jira issue on failure/error states, using a
// label-based dedup: it searches for an existing issue with the label
// "tekton-relay:{RunID}" before creating a new one.
type CreateIssueHandler struct {
	client     *Client
	projectKey string
	issueType  string
	log        *zap.Logger
}

// NewCreateIssueHandler builds a create_issue handler.
func NewCreateIssueHandler(client *Client, projectKey, issueType string, log *zap.Logger) (*CreateIssueHandler, error) {
	if projectKey == "" {
		return nil, fmt.Errorf("jira: create_issue project_key is required")
	}
	if issueType == "" {
		issueType = "Bug"
	}
	return &CreateIssueHandler{
		client:     client,
		projectKey: projectKey,
		issueType:  issueType,
		log:        log,
	}, nil
}

// Name returns the provider identifier.
func (h *CreateIssueHandler) Name() string { return notifierName }

// Provider returns the provider type identifier.
func (h *CreateIssueHandler) Provider() string { return notifierName }

// Type returns the action type.
func (h *CreateIssueHandler) Type() notifier.ActionType { return notifier.ActionJiraCreateIssue }

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *CreateIssueHandler) Close() error { return nil }

// Handle creates a Jira issue for failure/error states. Pending and running
// states are skipped. If an issue with the dedup label already exists, the
// creation is skipped.
func (h *CreateIssueHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.State != domain.StateFailure && e.State != domain.StateError {
		h.log.Debug("jira create_issue skipped: state not failure/error",
			zap.String("state", string(e.State)), zap.String("run", e.RunName))
		return nil
	}

	label := dedupeLabelPrefix + e.RunID

	exists, err := h.issueExistsByLabel(ctx, label)
	if err != nil {
		return fmt.Errorf("jira: search for dedup label %q: %w", label, err)
	}
	if exists {
		h.log.Debug("jira create_issue skipped: issue already exists",
			zap.String("label", label), zap.String("run", e.RunName))
		return nil
	}

	summary := fmt.Sprintf("Pipeline failure: %s", e.RunName)
	description := e.Description
	if description == "" {
		description = fmt.Sprintf("Pipeline %s failed in namespace %s", e.RunName, e.Namespace)
	}
	if e.TargetURL != "" {
		description += "\n\nLogs: " + e.TargetURL
	}

	payload := map[string]any{
		"fields": map[string]any{
			"project":     map[string]string{"key": h.projectKey},
			"summary":     summary,
			"description": description,
			"issuetype":   map[string]string{"name": h.issueType},
			"labels":      []string{label},
		},
	}

	var resp struct {
		Key string `json:"key"`
	}
	url := fmt.Sprintf("%s/rest/api/3/issue", h.client.BaseURL)
	if err := h.client.DoWithResponse(ctx, http.MethodPost, url, payload, &resp); err != nil {
		return fmt.Errorf("jira: create issue in %s: %w", h.projectKey, err)
	}

	h.log.Info("jira issue created",
		zap.String("issue", resp.Key),
		zap.String("label", label),
		zap.String("run", e.RunName))
	return nil
}

type searchResult struct {
	Total int `json:"total"`
}

// issueExistsByLabel searches Jira for an issue with the given label.
func (h *CreateIssueHandler) issueExistsByLabel(ctx context.Context, label string) (bool, error) {
	jql := fmt.Sprintf("labels = %q", label)
	searchURL := fmt.Sprintf("%s/rest/api/3/search?jql=%s&maxResults=1&fields=key", h.client.BaseURL, url.QueryEscape(jql))

	var result searchResult
	if err := h.client.DoWithResponse(ctx, http.MethodGet, searchURL, nil, &result); err != nil {
		return false, err
	}
	return result.Total > 0, nil
}
