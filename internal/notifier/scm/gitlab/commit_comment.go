package gitlab

import (
	"context"
	"fmt"
	"text/template"

	gl "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// CommitCommentHandler comments directly on a commit, covering pushes that
// have no associated merge request.
type CommitCommentHandler struct {
	client   *Client
	name     string
	template *template.Template
	log      *zap.Logger
}

// CommitCommentConfig configures the commit comment handler.
type CommitCommentConfig struct {
	Token              string
	BaseURL            string
	Name               string
	Template           string
	InsecureSkipVerify bool
	Log                *zap.Logger
}

// NewCommitCommentHandler creates a new GitLab commit comment handler.
func NewCommitCommentHandler(cfg CommitCommentConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("commit_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	c, err := NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, false, cfg.Log)
	if err != nil {
		return nil, err
	}

	return &CommitCommentHandler{
		client:   c,
		name:     cfg.Name,
		template: tmpl,
		log:      log,
	}, nil
}

// Name returns the handler name.
func (h *CommitCommentHandler) Name() string { return h.name }

// Type returns the action type.
func (h *CommitCommentHandler) Type() notifier.ActionType { return notifier.ActionCommitComment }

// Handle posts a comment on the commit itself.
func (h *CommitCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != h.name {
		return nil
	}
	if e.CommitSHA == "" {
		return nil
	}
	projectID, pErr := projectIdentifier(e)
	if pErr != nil {
		h.log.Warn("gitlab commit comment skipped: project cannot be identified",
			zap.String("run", e.RunName), zap.Error(pErr))
		return nil //nolint:nilerr // intentional: drop event if project cannot be identified
	}

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}
	if err := scm.Validate(h.name, "comment_body", body); err != nil {
		return err
	}

	_, _, err = h.client.gl.Commits.PostCommitComment(projectID, e.CommitSHA,
		&gl.PostCommitCommentOptions{Note: &body}, gl.WithContext(ctx))
	return err
}
