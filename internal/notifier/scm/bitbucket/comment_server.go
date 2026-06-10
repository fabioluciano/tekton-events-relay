package bitbucket

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// ServerCommentHandler posts comments to Bitbucket Server pull requests.
type ServerCommentHandler struct {
	client   *ServerClient
	template *template.Template
}

// ServerCommentConfig configures the Server comment handler.
type ServerCommentConfig struct {
	Token              string
	BaseURL            string
	Template           string
	InsecureSkipVerify bool
	Log                *zap.Logger
}

// NewServerCommentHandler creates a new Bitbucket Server PR comment handler.
func NewServerCommentHandler(cfg ServerCommentConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("pr_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	return &ServerCommentHandler{
		client:   NewServerClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, false, cfg.Log),
		template: tmpl,
	}, nil
}

// Name returns the handler name.
func (h *ServerCommentHandler) Name() string { return providerServer }

// Type returns the action type.
func (h *ServerCommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a comment to a Bitbucket Server PR.
func (h *ServerCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerServer {
		return nil
	}

	if e.PRNumber == nil || e.Repo.Project == "" || e.Repo.Name == "" {
		return nil
	}

	url := fmt.Sprintf("%s/rest/api/1.0/projects/%s/repos/%s/pull-requests/%d/comments",
		strings.TrimRight(h.client.BaseURL, "/"), e.Repo.Project, e.Repo.Name, *e.PRNumber)

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate(providerServer, "comment_body", body); err != nil {
		return err
	}

	payload := map[string]string{
		"text": body,
	}

	return h.client.Do(ctx, "POST", url, payload)
}
