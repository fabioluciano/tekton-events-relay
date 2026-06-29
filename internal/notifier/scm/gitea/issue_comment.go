//nolint:dupl // issue_comment and pr_comment share structure by design
package gitea

import (
	"context"
	"fmt"
	"text/template"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// IssueCommentHandler posts comments to Gitea issues.
type IssueCommentHandler struct {
	client   *Client
	name     string
	template *template.Template
	mode     string
	log      *zap.Logger
}

// IssueCommentConfig configures the issue comment handler.
type IssueCommentConfig struct {
	Client   *Client
	Name     string
	Template string
	Mode     string // scm.ModeCreate (default) or scm.ModeUpsert
	Log      *zap.Logger
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

	mode, err := scm.NormalizeMode(cfg.Mode)
	if err != nil {
		return nil, err
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}

	return &IssueCommentHandler{
		client:   cfg.Client,
		name:     cfg.Name,
		template: tmpl,
		mode:     mode,
		log:      log,
	}, nil
}

// Name returns the handler name.
func (h *IssueCommentHandler) Name() string { return h.name }

// Type returns the action type.
func (h *IssueCommentHandler) Type() notifier.ActionType { return notifier.ActionIssueComment }

// Handle posts a comment to a Gitea issue.
func (h *IssueCommentHandler) Handle(_ context.Context, e domain.Event) error {
	if e.Provider != h.name {
		return nil
	}

	if e.IssueNumber == nil || e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	return postComment(h.client, h.template, h.mode, h.log, e, int64(*e.IssueNumber), "issue_comment")
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *IssueCommentHandler) Close() error { return nil }
