package bitbucket

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

// ServerCommentHandler posts comments to Bitbucket Server pull requests.
type ServerCommentHandler struct {
	client   *ServerClient
	template *template.Template
	onStates []domain.State
}

// ServerCommentConfig configures the Server comment handler.
type ServerCommentConfig struct {
	Token              string
	BaseURL            string
	Template           string
	OnStates           []domain.State
	InsecureSkipVerify bool
}

// NewServerCommentHandler creates a new Bitbucket Server PR comment handler.
func NewServerCommentHandler(cfg ServerCommentConfig) notifier.ActionHandler {
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

	return &ServerCommentHandler{
		client:   NewServerClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify),
		template: tmpl,
		onStates: cfg.OnStates,
	}
}

func (h *ServerCommentHandler) Name() string              { return "bitbucket-server" }
func (h *ServerCommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a comment to a Bitbucket Server PR.
func (h *ServerCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != "bitbucket-server" {
		return nil
	}

	if len(h.onStates) > 0 && !contains(h.onStates, e.State) {
		return nil
	}

	if e.PRNumber == nil || e.Repo.Project == "" || e.Repo.Name == "" {
		return nil
	}

	url := fmt.Sprintf("%s/rest/api/1.0/projects/%s/repos/%s/pull-requests/%d/comments",
		strings.TrimRight(h.client.baseURL, "/"), e.Repo.Project, e.Repo.Name, *e.PRNumber)

	body, err := h.renderTemplate(e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate("bitbucket-server", "comment_body", body); err != nil {
		return err
	}

	payload := map[string]string{
		"text": body,
	}

	return h.client.Do(ctx, "POST", url, payload)
}

func (h *ServerCommentHandler) renderTemplate(e domain.Event) (string, error) {
	if h.template == nil {
		return fmt.Sprintf("Build %s for %s", e.State, e.RunName), nil
	}

	var buf bytes.Buffer
	if err := h.template.Execute(&buf, e); err != nil {
		return "", err
	}
	return buf.String(), nil
}
