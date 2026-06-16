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

// MRCommentHandler posts notes (comments) to GitLab merge requests.
type MRCommentHandler struct {
	client   *Client
	name     string
	template *template.Template
	mode     string
	log      *zap.Logger
}

// MRCommentConfig configures the MR comment handler.
type MRCommentConfig struct {
	Client   *Client
	Name     string
	Template string
	Mode     string // scm.ModeCreate (default) or scm.ModeUpsert
	Log      *zap.Logger
}

// NewMRCommentHandler creates a new GitLab merge request comment handler.
func NewMRCommentHandler(cfg MRCommentConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("pr_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	mode, err := scm.NormalizeMode(cfg.Mode)
	if err != nil {
		return nil, err
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}

	return &MRCommentHandler{
		client:   cfg.Client,
		name:     cfg.Name,
		template: tmpl,
		mode:     mode,
		log:      log,
	}, nil
}

// Name returns the handler name.
func (h *MRCommentHandler) Name() string { return h.name }

// Type returns the action type.
func (h *MRCommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a note to a GitLab merge request. The PR number annotation
// carries the MR IID.
func (h *MRCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != h.name {
		return nil
	}

	if e.PRNumber == nil {
		return nil
	}

	projectID, pErr := projectIdentifier(e)
	if pErr != nil {
		h.log.Warn("gitlab mr comment skipped: project cannot be identified",
			zap.String("run", e.RunName),
			zap.Error(pErr))
		return nil //nolint:nilerr // intentional: drop event if project cannot be identified
	}

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if h.mode == scm.ModeUpsert {
		marker := scm.Marker(e.RunID, "pr_comment")
		body = scm.WithMarker(marker, body)
		if err := scm.Validate(h.name, "comment_body", body); err != nil {
			return err
		}
		return h.upsertNote(ctx, projectID, int64(*e.PRNumber), marker, body)
	}

	if err := scm.Validate(h.name, "comment_body", body); err != nil {
		return err
	}

	_, _, err = h.client.gl.Notes.CreateMergeRequestNote(projectID, int64(*e.PRNumber),
		&gl.CreateMergeRequestNoteOptions{Body: &body}, gl.WithContext(ctx))
	return err
}

// upsertNote edits the existing relay-managed note carrying the marker, or
// creates one if absent. Listing failures fall back to create so an API
// hiccup never blocks the notification.
func (h *MRCommentHandler) upsertNote(ctx context.Context, projectID string, mrIID int64, marker, body string) error {
	notes, _, err := h.client.gl.Notes.ListMergeRequestNotes(projectID, mrIID,
		&gl.ListMergeRequestNotesOptions{ListOptions: gl.ListOptions{PerPage: 100}},
		gl.WithContext(ctx))
	if err != nil {
		h.log.Warn("upsert: listing notes failed, falling back to create", zap.Error(err))
	} else {
		for _, n := range notes {
			if n != nil && scm.HasMarker(n.Body, marker) {
				_, _, err := h.client.gl.Notes.UpdateMergeRequestNote(projectID, mrIID, n.ID,
					&gl.UpdateMergeRequestNoteOptions{Body: &body}, gl.WithContext(ctx))
				return err
			}
		}
	}

	_, _, err = h.client.gl.Notes.CreateMergeRequestNote(projectID, mrIID,
		&gl.CreateMergeRequestNoteOptions{Body: &body}, gl.WithContext(ctx))
	return err
}
