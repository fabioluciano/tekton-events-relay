//nolint:dupl // similar structure to IssueCommentHandler but different endpoint semantics
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

// PRCommentHandler posts comments to GitHub pull requests.
type PRCommentHandler struct {
	client   *Client
	template *template.Template
}

// PRCommentConfig configures the PR comment handler.
type PRCommentConfig struct {
	Token              string
	BaseURL            string
	Template           string
	InsecureSkipVerify bool
}

// NewPRCommentHandler creates a new GitHub PR comment handler.
func NewPRCommentHandler(cfg PRCommentConfig, log *zap.Logger) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("pr_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	return &PRCommentHandler{
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, log, false),
		template: tmpl,
	}, nil
}

// Name returns the handler name.
func (h *PRCommentHandler) Name() string { return providerGitHub }

// Type returns the action type.
func (h *PRCommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a comment to a GitHub PR.
func (h *PRCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerGitHub {
		return nil
	}

	if e.PRNumber == nil {
		return nil
	}

	if e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	// GitHub PRs use issues API for comments
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments",
		h.client.baseURL, e.Repo.Owner, e.Repo.Name, *e.PRNumber)

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
