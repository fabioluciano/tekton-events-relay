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

// IssueCommentHandler posts comments to GitHub issues.
type IssueCommentHandler struct {
	client   *Client
	template *template.Template
	onStates []domain.State // Only comment on these states
}

// IssueCommentConfig configures the issue comment handler.
type IssueCommentConfig struct {
	Token              string
	BaseURL            string
	Template           string
	OnStates           []domain.State
	InsecureSkipVerify bool
}

// NewIssueCommentHandler creates a new GitHub issue comment handler.
func NewIssueCommentHandler(cfg IssueCommentConfig) notifier.ActionHandler {
	// Inject custom template functions for cross-references
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
			tmpl = nil // Log error in production; for now return handler with nil template
		}
	}

	return &IssueCommentHandler{
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify),
		template: tmpl,
		onStates: cfg.OnStates,
	}
}

func (h *IssueCommentHandler) Name() string              { return "github" }
func (h *IssueCommentHandler) Type() notifier.ActionType { return notifier.ActionIssueComment }

// Handle posts a comment to a GitHub issue. Returns nil (skip) if provider doesn't match, state not in filter, or issue number unavailable.
func (h *IssueCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	// Provider-match guard
	if e.Provider != "github" {
		return nil
	}

	// State filter: skip if state not in configured list
	if len(h.onStates) > 0 && !contains(h.onStates, e.State) {
		return nil
	}

	// Skip if no issue number available
	if e.IssueNumber == nil {
		return nil // Silently skip - user didn't provide annotation or API lookup disabled
	}

	// Validate required fields
	if e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments",
		h.client.baseURL, e.Repo.Owner, e.Repo.Name, *e.IssueNumber)

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
