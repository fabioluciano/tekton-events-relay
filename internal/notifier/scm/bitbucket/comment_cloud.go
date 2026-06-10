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

// CloudCommentHandler posts comments to Bitbucket Cloud pull requests.
type CloudCommentHandler struct {
	client   *CloudClient
	template *template.Template
}

// CloudCommentConfig configures the Cloud comment handler.
type CloudCommentConfig struct {
	Username           string
	AppPassword        string
	BaseURL            string
	Template           string
	InsecureSkipVerify bool
	Log                *zap.Logger
}

// NewCloudCommentHandler creates a new Bitbucket Cloud PR comment handler.
func NewCloudCommentHandler(cfg CloudCommentConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("pr_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	return &CloudCommentHandler{
		client:   NewCloudClient(cfg.Username, cfg.AppPassword, cfg.BaseURL, cfg.InsecureSkipVerify, false, cfg.Log),
		template: tmpl,
	}, nil
}

// Name returns the handler name.
func (h *CloudCommentHandler) Name() string { return providerCloud }

// Type returns the action type.
func (h *CloudCommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a comment to a Bitbucket Cloud PR.
func (h *CloudCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerCloud {
		return nil
	}

	if e.PRNumber == nil {
		return nil
	}

	ws := e.Repo.Workspace
	if ws == "" {
		ws = e.Repo.Owner
	}
	if ws == "" || e.Repo.Name == "" {
		return nil
	}

	url := fmt.Sprintf("%s/2.0/repositories/%s/%s/pullrequests/%d/comments",
		strings.TrimRight(h.client.BaseURL, "/"), ws, e.Repo.Name, *e.PRNumber)

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate(providerCloud, "comment_body", body); err != nil {
		return err
	}

	payload := map[string]any{
		"content": map[string]string{
			"raw": body,
		},
	}

	return h.client.Do(ctx, "POST", url, payload)
}
