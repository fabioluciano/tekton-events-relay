package azuredevops

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

// CommentHandler posts comments to Azure DevOps pull requests.
type CommentHandler struct {
	client   *Client
	template *template.Template
	onStates []domain.State
}

// CommentConfig configures the comment handler.
type CommentConfig struct {
	Token              string
	BaseURL            string
	Genre              string
	Template           string
	OnStates           []domain.State
	InsecureSkipVerify bool
}

// NewCommentHandler creates a new Azure DevOps PR comment handler.
func NewCommentHandler(cfg CommentConfig) notifier.ActionHandler {
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

	return &CommentHandler{
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.Genre, cfg.InsecureSkipVerify),
		template: tmpl,
		onStates: cfg.OnStates,
	}
}

func (h *CommentHandler) Name() string              { return "azure-devops" }
func (h *CommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a comment to an Azure DevOps PR.
func (h *CommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != "azure-devops" {
		return nil
	}

	if len(h.onStates) > 0 && !contains(h.onStates, e.State) {
		return nil
	}

	if e.PRNumber == nil || e.Repo.Org == "" || e.Repo.Project == "" || e.Repo.Name == "" {
		return nil
	}

	url := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullRequests/%d/threads?api-version=7.1",
		strings.TrimRight(h.client.baseURL, "/"),
		e.Repo.Org, e.Repo.Project, e.Repo.Name, *e.PRNumber)

	body, err := h.renderTemplate(e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate("azure-devops", "comment_body", body); err != nil {
		return err
	}

	payload := map[string]any{
		"comments": []map[string]string{
			{
				"content":     body,
				"commentType": "text",
			},
		},
		"status": "active",
	}

	return h.client.Do(ctx, "POST", url, payload)
}

func (h *CommentHandler) renderTemplate(e domain.Event) (string, error) {
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
