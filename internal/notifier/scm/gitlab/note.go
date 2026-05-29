package gitlab

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

// NoteHandler posts notes (comments) to GitLab issues and merge requests.
type NoteHandler struct {
	client     *Client
	name       string
	template   *template.Template
	onStates   []domain.State
	noteType   notifier.ActionType
}

// NoteConfig configures the note handler.
type NoteConfig struct {
	Token              string
	BaseURL            string
	Name               string
	Template           string
	OnStates           []domain.State
	NoteType           notifier.ActionType
	InsecureSkipVerify bool
}

// NewNoteHandler creates a new GitLab note handler.
func NewNoteHandler(cfg NoteConfig) notifier.ActionHandler {
	funcMap := template.FuncMap{
		"IssueRef":    scm.FormatIssueRef,
		"PRRef":       scm.FormatPRRef,
		"UserMention": scm.FormatUserMention,
		"Truncate":    scm.Truncate,
	}

	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = template.New("note").Funcs(funcMap).Parse(cfg.Template)
		if err != nil {
			tmpl = nil
		}
	}

	return &NoteHandler{
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify),
		name:     cfg.Name,
		template: tmpl,
		onStates: cfg.OnStates,
		noteType: cfg.NoteType,
	}
}

func (h *NoteHandler) Name() string              { return h.name }
func (h *NoteHandler) Type() notifier.ActionType { return h.noteType }

// Handle posts a note to a GitLab issue or merge request.
func (h *NoteHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != h.name {
		return nil
	}

	if len(h.onStates) > 0 && !contains(h.onStates, e.State) {
		return nil
	}

	projectID, err := projectIdentifier(e)
	if err != nil {
		return nil
	}

	var notableType string
	var notableID int
	if h.noteType == notifier.ActionIssueComment && e.IssueNumber != nil {
		notableType = "issues"
		notableID = *e.IssueNumber
	} else if h.noteType == notifier.ActionPRComment && e.PRNumber != nil {
		notableType = "merge_requests"
		notableID = *e.PRNumber
	} else {
		return nil
	}

	url := fmt.Sprintf("%s/projects/%s/%s/%d/notes",
		strings.TrimRight(h.client.baseURL, "/"), projectID, notableType, notableID)

	body, err := h.renderTemplate(e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate(h.name, "comment_body", body); err != nil {
		return fmt.Errorf("validate comment body: %w", err)
	}

	payload := map[string]string{"body": body}
	return h.client.Do(ctx, "POST", url, payload)
}

func (h *NoteHandler) renderTemplate(e domain.Event) (string, error) {
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
