//nolint:dupl // issue_comment and mr_comment share structure by design
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

// IssueCommentHandler posts notes (comments) to GitLab issues.
type IssueCommentHandler struct {
	client   *Client
	name     string
	template *template.Template
	mode     string
	log      *zap.Logger
}

// IssueCommentConfig configures the issue comment handler.
type IssueCommentConfig struct {
	Client   *Client
	Name     string
	Template string
	Mode     string // scm.ModeCreate (default) or scm.ModeUpsert
	Log      *zap.Logger
}

// NewIssueCommentHandler creates a new GitLab issue comment handler.
func NewIssueCommentHandler(cfg IssueCommentConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("issue_comment", cfg.Template, nil)
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

	return &IssueCommentHandler{
		client:   cfg.Client,
		name:     cfg.Name,
		template: tmpl,
		mode:     mode,
		log:      log,
	}, nil
}

// Name returns the handler name.
func (h *IssueCommentHandler) Name() string { return h.name }

// Provider returns the provider type identifier.
func (h *IssueCommentHandler) Provider() string { return providerGitLab }

// Type returns the action type.
func (h *IssueCommentHandler) Type() notifier.ActionType { return notifier.ActionIssueComment }

// Handle posts a note to a GitLab issue. The issue number annotation carries
// the issue IID.
func (h *IssueCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerGitLab {
		return nil
	}

	if e.IssueNumber == nil {
		return nil
	}

	projectID, pErr := projectIdentifier(e)
	if pErr != nil {
		h.log.Warn("gitlab issue comment skipped: project cannot be identified",
			zap.String("run", e.RunName),
			zap.Error(pErr))
		return nil //nolint:nilerr // intentional: drop event if project cannot be identified
	}

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if h.mode == scm.ModeUpsert {
		marker := scm.Marker(e.RunID, "issue_comment")
		body = scm.WithMarker(marker, body)
		if err := scm.Validate(h.name, "comment_body", body); err != nil {
			return err
		}
		return h.upsertNote(ctx, projectID, int64(*e.IssueNumber), marker, body)
	}

	if err := scm.Validate(h.name, "comment_body", body); err != nil {
		return err
	}

	_, _, err = h.client.gl.Notes.CreateIssueNote(projectID, int64(*e.IssueNumber),
		&gl.CreateIssueNoteOptions{Body: &body}, gl.WithContext(ctx))
	return err
}

// upsertNote edits the existing relay-managed note carrying the marker, or
// creates one if absent. Listing failures fall back to create so an API
// hiccup never blocks the notification.
func (h *IssueCommentHandler) upsertNote(ctx context.Context, projectID string, issueIID int64, marker, body string) error {
	notes, _, err := h.client.gl.Notes.ListIssueNotes(projectID, issueIID,
		&gl.ListIssueNotesOptions{ListOptions: gl.ListOptions{PerPage: 100}},
		gl.WithContext(ctx))
	if err != nil {
		h.log.Warn("upsert: listing notes failed, falling back to create", zap.Error(err))
	} else {
		for _, n := range notes {
			if n != nil && scm.HasMarker(n.Body, marker) {
				_, _, err := h.client.gl.Notes.UpdateIssueNote(projectID, issueIID, n.ID,
					&gl.UpdateIssueNoteOptions{Body: &body}, gl.WithContext(ctx))
				return err
			}
		}
	}

	_, _, err = h.client.gl.Notes.CreateIssueNote(projectID, issueIID,
		&gl.CreateIssueNoteOptions{Body: &body}, gl.WithContext(ctx))
	return err
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *IssueCommentHandler) Close() error { return nil }
