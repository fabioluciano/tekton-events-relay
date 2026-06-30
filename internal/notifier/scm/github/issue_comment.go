//nolint:dupl // issue_comment and pr_comment share structure by design
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
	name     string
	client   HTTPDoer
	template *template.Template
	mode     string
	log      *zap.Logger
}

// IssueCommentConfig configures the issue comment handler.
type IssueCommentConfig struct {
	Name     string
	Client   HTTPDoer
	Template string
	Mode     string // scm.ModeCreate (default) or scm.ModeUpsert
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

	mode, err := scm.NormalizeMode(cfg.Mode)
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = zap.NewNop()
	}

	return &IssueCommentHandler{
		name:     cfg.Name,
		client:   cfg.Client,
		template: tmpl,
		mode:     mode,
		log:      log,
	}, nil
}

// Name returns the handler name.
func (h *IssueCommentHandler) Name() string { return h.name }

// Provider returns the provider type identifier.
func (h *IssueCommentHandler) Provider() string { return providerGitHub }

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

	return postIssueComment(ctx, h.client, h.template, h.mode, h.log, e, *e.IssueNumber, "issue_comment")
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *IssueCommentHandler) Close() error { return nil }
