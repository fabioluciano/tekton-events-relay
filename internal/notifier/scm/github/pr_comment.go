//nolint:dupl // pr_comment and issue_comment share structure by design
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
	client   HTTPDoer
	template *template.Template
	mode     string
	log      *zap.Logger
}

// PRCommentConfig configures the PR comment handler.
type PRCommentConfig struct {
	Client   HTTPDoer
	Template string
	Mode     string // scm.ModeCreate (default) or scm.ModeUpsert
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

	mode, err := scm.NormalizeMode(cfg.Mode)
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = zap.NewNop()
	}

	return &PRCommentHandler{
		client:   cfg.Client,
		template: tmpl,
		mode:     mode,
		log:      log,
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

	// GitHub PRs use the issues API for comments.
	return postIssueComment(ctx, h.client, h.template, h.mode, h.log, e, *e.PRNumber, "pr_comment")
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *PRCommentHandler) Close() error { return nil }
