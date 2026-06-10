//nolint:dupl // similar structure to IssueCommentHandler but different endpoint semantics
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

// PRCommentHandler posts comments to Gitea pull requests.
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
	Log                *zap.Logger
}

// NewPRCommentHandler creates a new Gitea PR comment handler.
func NewPRCommentHandler(cfg PRCommentConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("pr_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	return &PRCommentHandler{
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, false, cfg.Log),
		template: tmpl,
	}, nil
}

// Name returns the handler name.
func (h *PRCommentHandler) Name() string { return providerGitea }

// Type returns the action type.
func (h *PRCommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a comment to a Gitea PR.
//
//nolint:dupl // similar structure to IssueCommentHandler but different endpoint semantics
func (h *PRCommentHandler) Handle(_ context.Context, e domain.Event) error {
	if e.Provider != providerGitea {
		return nil
	}

	if e.PRNumber == nil || e.Repo.Owner == "" || e.Repo.Name == "" {
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

	_, _, err = h.client.sdk.CreateIssueComment(e.Repo.Owner, e.Repo.Name, int64(*e.PRNumber), opts)
	return err
}
