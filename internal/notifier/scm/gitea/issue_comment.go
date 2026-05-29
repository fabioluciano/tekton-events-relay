package gitea

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// IssueCommentHandler posts comments to Gitea issues.
type IssueCommentHandler struct {
	client   *Client
	template *template.Template
	onStates []domain.State
}

// IssueCommentConfig configures the issue comment handler.
type IssueCommentConfig struct {
	Token              string
	BaseURL            string
	Template           string
	OnStates           []domain.State
	InsecureSkipVerify bool
}

// NewIssueCommentHandler creates a new Gitea issue comment handler.
func NewIssueCommentHandler(cfg IssueCommentConfig) notifier.ActionHandler {
	funcMap := template.FuncMap{
		"IssueRef":    scm.FormatIssueRef,
		"PRRef":       scm.FormatPRRef,
		"UserMention": scm.FormatUserMention,
		"Truncate":    scm.Truncate,
	}

	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = template.New("issue_comment").Funcs(funcMap).Parse(cfg.Template)
		if err != nil {
			tmpl = nil
		}
	}

	return &IssueCommentHandler{
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify),
		template: tmpl,
		onStates: cfg.OnStates,
	}
}

func (h *IssueCommentHandler) Name() string              { return "gitea" }
func (h *IssueCommentHandler) Type() notifier.ActionType { return notifier.ActionIssueComment }

// Handle posts a comment to a Gitea issue.
func (h *IssueCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != "gitea" {
		return nil
	}

	if len(h.onStates) > 0 && !contains(h.onStates, e.State) {
		return nil
	}

	if e.IssueNumber == nil || e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/issues/%d/comments",
		strings.TrimRight(h.client.baseURL, "/"),
		e.Repo.Owner, e.Repo.Name, *e.IssueNumber)

	body, err := h.renderTemplate(e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate("gitea", "comment_body", body); err != nil {
		return fmt.Errorf("validate comment body: %w", err)
	}

	payload := map[string]string{"body": body}
	return h.client.Do(ctx, "POST", url, payload)
}

func (h *IssueCommentHandler) renderTemplate(e domain.Event) (string, error) {
	if h.template == nil {
		return fmt.Sprintf("Build %s for %s", e.State, e.RunName), nil
	}

	var buf bytes.Buffer
	if err := h.template.Execute(&buf, e); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func contains(states []domain.State, s domain.State) bool {
	for _, state := range states {
		if state == s {
			return true
		}
	}
	return false
}
