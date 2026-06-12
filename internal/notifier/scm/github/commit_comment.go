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

// CommitCommentHandler comments directly on a commit, covering pushes that
// have no associated pull request (where pr_comment silently skips).
type CommitCommentHandler struct {
	client   *Client
	template *template.Template
	log      *zap.Logger
}

// CommitCommentConfig configures the commit comment handler.
type CommitCommentConfig struct {
	Token              string
	BaseURL            string
	Template           string
	InsecureSkipVerify bool
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
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, log, false),
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

	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s/comments",
		h.client.baseURL, e.Repo.Owner, e.Repo.Name, e.CommitSHA)
	return h.client.Do(ctx, "POST", url, map[string]string{bodyFieldKey: body})
}
