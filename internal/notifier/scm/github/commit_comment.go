package github

import (
	"context"
	"fmt"
	"text/template"

	gh "github.com/google/go-github/v68/github"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// CommitCommentHandler comments directly on a commit, covering pushes that
// have no associated pull request (where pr_comment silently skips).
type CommitCommentHandler struct {
	client   HTTPDoer
	template *template.Template
	log      *zap.Logger
}

// CommitCommentConfig configures the commit comment handler.
type CommitCommentConfig struct {
	Client   HTTPDoer
	Template string
}

// NewCommitCommentHandler creates a new GitHub commit comment handler.
func NewCommitCommentHandler(cfg CommitCommentConfig, log *zap.Logger) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("commit_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &CommitCommentHandler{
		client:   cfg.Client,
		template: tmpl,
		log:      log,
	}, nil
}

// Name returns the handler name.
func (h *CommitCommentHandler) Name() string { return providerGitHub }

// Type returns the action type.
func (h *CommitCommentHandler) Type() notifier.ActionType { return notifier.ActionCommitComment }

// Handle posts a comment on the commit itself.
func (h *CommitCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerGitHub {
		return nil
	}
	if e.CommitSHA == "" || e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}
	if err := scm.Validate(providerGitHub, "comment_body", body); err != nil {
		return err
	}

	comment := &gh.RepositoryComment{Body: gh.Ptr(body)}
	_, _, err = h.client.GH().Repositories.CreateComment(ctx, e.Repo.Owner, e.Repo.Name, e.CommitSHA, comment)
	return err
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *CommitCommentHandler) Close() error { return nil }
