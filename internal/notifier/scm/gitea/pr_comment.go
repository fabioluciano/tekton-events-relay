//nolint:dupl // pr_comment and issue_comment share structure by design
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

// PRCommentHandler posts comments to Gitea pull requests.
type PRCommentHandler struct {
	client   *Client
	name     string
	template *template.Template
	mode     string
	log      *zap.Logger
}

// PRCommentConfig configures the PR comment handler.
type PRCommentConfig struct {
	Client   *Client
	Name     string
	Template string
	Mode     string // scm.ModeCreate (default) or scm.ModeUpsert
	Log      *zap.Logger
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

	mode, err := scm.NormalizeMode(cfg.Mode)
	if err != nil {
		return nil, err
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}

	return &PRCommentHandler{
		client:   cfg.Client,
		name:     cfg.Name,
		template: tmpl,
		mode:     mode,
		log:      log,
	}, nil
}

// Name returns the handler name.
func (h *PRCommentHandler) Name() string { return h.name }

// Type returns the action type.
func (h *PRCommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a comment to a Gitea PR.
func (h *PRCommentHandler) Handle(_ context.Context, e domain.Event) error {
	if e.Provider != h.name {
		return nil
	}

	if e.PRNumber == nil || e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	return postComment(h.client, h.template, h.mode, h.log, e, int64(*e.PRNumber), "pr_comment")
}
