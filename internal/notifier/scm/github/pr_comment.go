package github

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// PRCommentHandler posts comments to GitHub pull requests.
type PRCommentHandler struct {
	client   *Client
	template *template.Template
	onStates []domain.State
}

// PRCommentConfig configures the PR comment handler.
type PRCommentConfig struct {
	Token              string
	BaseURL            string
	Template           string
	OnStates           []domain.State
	InsecureSkipVerify bool
}

// NewPRCommentHandler creates a new GitHub PR comment handler.
func NewPRCommentHandler(cfg PRCommentConfig) notifier.ActionHandler {
	funcMap := template.FuncMap{
		"IssueRef":    scm.FormatIssueRef,
		"PRRef":       scm.FormatPRRef,
		"UserMention": scm.FormatUserMention,
		"Truncate":    scm.Truncate,
	}

	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = template.New("pr_comment").Funcs(funcMap).Parse(cfg.Template)
		if err != nil {
			tmpl = nil
		}
	}

	return &PRCommentHandler{
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify),
		template: tmpl,
		onStates: cfg.OnStates,
	}
}

func (h *PRCommentHandler) Name() string              { return "github" }
func (h *PRCommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a comment to a GitHub PR.
func (h *PRCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != "github" {
		return nil
	}

	if len(h.onStates) > 0 && !contains(h.onStates, e.State) {
		return nil
	}

	if e.PRNumber == nil {
		return nil
	}

	if e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	// GitHub PRs use issues API for comments
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments",
		h.client.baseURL, e.Repo.Owner, e.Repo.Name, *e.PRNumber)

	body, err := h.renderTemplate(e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate("github", "comment_body", body); err != nil {
		return err
	}

	payload := map[string]string{"body": body}
	return h.client.Do(ctx, "POST", url, payload)
}

func (h *PRCommentHandler) renderTemplate(e domain.Event) (string, error) {
	if h.template == nil {
		return fmt.Sprintf("Build %s for %s", e.State, e.RunName), nil
	}

	var buf bytes.Buffer
	if err := h.template.Execute(&buf, e); err != nil {
		return "", err
	}
	return buf.String(), nil
}
