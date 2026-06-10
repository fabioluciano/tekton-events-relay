//nolint:dupl // similar structure to PRCommentHandler but different endpoint semantics
package gitea

import (
	"context"
	"fmt"
	"text/template"

	giteaSDK "code.gitea.io/sdk/gitea"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// IssueCommentHandler posts comments to Gitea issues.
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
	Log                *zap.Logger
}

// NewIssueCommentHandler creates a new Gitea issue comment handler.
func NewIssueCommentHandler(cfg IssueCommentConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("issue_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	return &IssueCommentHandler{
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, false, cfg.Log),
		template: tmpl,
	}, nil
}

// Name returns the handler name.
func (h *IssueCommentHandler) Name() string { return providerGitea }

// Type returns the action type.
func (h *IssueCommentHandler) Type() notifier.ActionType { return notifier.ActionIssueComment }

// Handle posts a comment to a Gitea issue.
//
//nolint:dupl // similar structure to PRCommentHandler but different endpoint semantics
func (h *IssueCommentHandler) Handle(_ context.Context, e domain.Event) error {
	if e.Provider != providerGitea {
		return nil
	}

	if e.IssueNumber == nil || e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate(providerGitea, "comment_body", body); err != nil {
		return fmt.Errorf("validate comment body: %w", err)
	}

	opts := giteaSDK.CreateIssueCommentOption{
		Body: body,
	}

	_, _, err = h.client.sdk.CreateIssueComment(e.Repo.Owner, e.Repo.Name, int64(*e.IssueNumber), opts)
	return err
}
