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

// NoteHandler posts notes (comments) to GitLab issues and merge requests.
type NoteHandler struct {
	client   *Client
	name     string
	template *template.Template
	noteType notifier.ActionType
}

// NoteConfig configures the note handler.
type NoteConfig struct {
	Token              string
	BaseURL            string
	Name               string
	Template           string
	NoteType           notifier.ActionType
	InsecureSkipVerify bool
	Log                *zap.Logger
}

// NewNoteHandler creates a new GitLab note handler.
func NewNoteHandler(cfg NoteConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("note", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	return &NoteHandler{
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, false, cfg.Log),
		name:     cfg.Name,
		template: tmpl,
		noteType: cfg.NoteType,
	}, nil
}

// Name returns the handler name.
func (h *NoteHandler) Name() string { return h.name }

// Type returns the action type.
func (h *NoteHandler) Type() notifier.ActionType { return h.noteType }

// Handle posts a note to a GitLab issue or merge request.
func (h *NoteHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != h.name {
		return nil
	}

	projectID, pErr := projectIdentifier(e)
	if pErr != nil {
		return nil //nolint:nilerr // intentional: drop event if project cannot be identified
	}

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate(h.name, "comment_body", body); err != nil {
		return fmt.Errorf("validate comment body: %w", err)
	}

	switch {
	case h.noteType == notifier.ActionIssueComment && e.IssueNumber != nil:
		opts := &gl.CreateIssueNoteOptions{Body: &body}
		_, _, err = h.client.gl.Notes.CreateIssueNote(projectID, int64(*e.IssueNumber), opts, gl.WithContext(ctx))
		return err
	case h.noteType == notifier.ActionPRComment && e.PRNumber != nil:
		opts := &gl.CreateMergeRequestNoteOptions{Body: &body}
		_, _, err = h.client.gl.Notes.CreateMergeRequestNote(projectID, int64(*e.PRNumber), opts, gl.WithContext(ctx))
		return err
	default:
		return nil
	}
}
