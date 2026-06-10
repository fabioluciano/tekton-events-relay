//nolint:dupl // similar structure to PRCommentHandler but different endpoint semantics
package github

import (
	"context"
	"fmt"
	"text/template"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// IssueCommentHandler posts comments to GitHub issues.
type IssueCommentHandler struct {
	client   *Client
	template *template.Template
}

// IssueCommentConfig configures the issue comment handler.
type IssueCommentConfig struct {
	Token              string
	BaseURL            string
	Template           string
	InsecureSkipVerify bool
}

// NewIssueCommentHandler creates a new GitHub issue comment handler.
func NewIssueCommentHandler(cfg IssueCommentConfig, log *zap.Logger) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("issue_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	return &IssueCommentHandler{
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, log, false),
		template: tmpl,
	}, nil
}

// Name returns the handler name.
func (h *IssueCommentHandler) Name() string { return providerGitHub }

// Type returns the action type.
func (h *IssueCommentHandler) Type() notifier.ActionType { return notifier.ActionIssueComment }

// Handle posts a comment to a GitHub issue. Returns nil (skip) if provider doesn't match or issue number unavailable.
func (h *IssueCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	// Provider-match guard
	if e.Provider != providerGitHub {
		return nil
	}

	// Skip if no issue number available
	if e.IssueNumber == nil {
		return nil // Silently skip - user didn't provide annotation or API lookup disabled
	}

	// Validate required fields
	if e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments",
		h.client.baseURL, e.Repo.Owner, e.Repo.Name, *e.IssueNumber)

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate(providerGitHub, "comment_body", body); err != nil {
		return err
	}

	payload := map[string]string{"body": body}
	return h.client.Do(ctx, "POST", url, payload)
}
