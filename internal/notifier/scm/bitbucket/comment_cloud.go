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

// CloudCommentHandler posts comments to Bitbucket Cloud pull requests.
type CloudCommentHandler struct {
	client   *CloudClient
	template *template.Template
	onStates []domain.State
}

// CloudCommentConfig configures the Cloud comment handler.
type CloudCommentConfig struct {
	Username           string
	AppPassword        string
	BaseURL            string
	Template           string
	OnStates           []domain.State
	InsecureSkipVerify bool
}

// NewCloudCommentHandler creates a new Bitbucket Cloud PR comment handler.
func NewCloudCommentHandler(cfg CloudCommentConfig) notifier.ActionHandler {
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

	return &CloudCommentHandler{
		client:   NewCloudClient(cfg.Username, cfg.AppPassword, cfg.BaseURL, cfg.InsecureSkipVerify),
		template: tmpl,
		onStates: cfg.OnStates,
	}
}

func (h *CloudCommentHandler) Name() string              { return "bitbucket-cloud" }
func (h *CloudCommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a comment to a Bitbucket Cloud PR.
func (h *CloudCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != "bitbucket-cloud" {
		return nil
	}

	if len(h.onStates) > 0 && !contains(h.onStates, e.State) {
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
		strings.TrimRight(h.client.baseURL, "/"), ws, e.Repo.Name, *e.PRNumber)

	body, err := h.renderTemplate(e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate("bitbucket-cloud", "comment_body", body); err != nil {
		return err
	}

	payload := map[string]any{
		"content": map[string]string{
			"raw": body,
		},
	}

	return h.client.Do(ctx, "POST", url, payload)
}

func (h *CloudCommentHandler) renderTemplate(e domain.Event) (string, error) {
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
