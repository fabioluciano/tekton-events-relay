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

// CommitCommentHandler comments directly on a commit, covering pushes that
// have no associated merge request.
type CommitCommentHandler struct {
	client   *Client
	name     string
	template *template.Template
	mode     string
	log      *zap.Logger
}

// CommitCommentConfig configures the commit comment handler.
type CommitCommentConfig struct {
	Client   *Client
	Name     string
	Template string
	Mode     string // scm.ModeCreate (default) or scm.ModeUpsert
	Log      *zap.Logger
}

// NewCommitCommentHandler creates a new GitLab commit comment handler.
func NewCommitCommentHandler(cfg CommitCommentConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("commit_comment", cfg.Template, nil)
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

	return &CommitCommentHandler{
		client:   cfg.Client,
		name:     cfg.Name,
		template: tmpl,
		mode:     mode,
		log:      log,
	}, nil
}

// Name returns the handler name.
func (h *CommitCommentHandler) Name() string { return h.name }

// Provider returns the provider type identifier.
func (h *CommitCommentHandler) Provider() string { return providerGitLab }

// Type returns the action type.
func (h *CommitCommentHandler) Type() notifier.ActionType { return notifier.ActionCommitComment }

// Handle posts a comment on the commit itself.
func (h *CommitCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerGitLab {
		return nil
	}
	if e.CommitSHA == "" {
		return nil
	}
	projectID, pErr := projectIdentifier(e)
	if pErr != nil {
		h.log.Warn("gitlab commit comment skipped: project cannot be identified",
			zap.String("run", e.RunName), zap.Error(pErr))
		return nil //nolint:nilerr // intentional: drop event if project cannot be identified
	}

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if h.mode == scm.ModeUpsert {
		marker := scm.Marker(e.RunID, "commit_comment")
		body = scm.WithMarker(marker, body)
		if err := scm.Validate(h.name, "comment_body", body); err != nil {
			return err
		}
		return h.upsertCommitNote(ctx, projectID, e.CommitSHA, marker, body)
	}

	if err := scm.Validate(h.name, "comment_body", body); err != nil {
		return err
	}

	_, _, err = h.client.gl.Commits.PostCommitComment(projectID, e.CommitSHA,
		&gl.PostCommitCommentOptions{Note: &body}, gl.WithContext(ctx))
	return err
}

// upsertCommitNote edits the existing relay-managed commit discussion note
// carrying the marker, or creates a new commit discussion if absent. Listing
// failures fall back to create so an API hiccup never blocks the notification.
func (h *CommitCommentHandler) upsertCommitNote(ctx context.Context, projectID, commitSHA, marker, body string) error {
	discussions, _, err := h.client.gl.Discussions.ListCommitDiscussions(projectID, commitSHA,
		&gl.ListCommitDiscussionsOptions{ListOptions: gl.ListOptions{PerPage: 100}},
		gl.WithContext(ctx))
	if err != nil {
		h.log.Warn("upsert: listing commit discussions failed, falling back to create", zap.Error(err))
	} else {
		for _, d := range discussions {
			if d == nil {
				continue
			}
			for _, n := range d.Notes {
				if n != nil && scm.HasMarker(n.Body, marker) {
					_, _, uErr := h.client.gl.Discussions.UpdateCommitDiscussionNote(projectID, commitSHA, d.ID, n.ID,
						&gl.UpdateCommitDiscussionNoteOptions{Body: &body}, gl.WithContext(ctx))
					return uErr
				}
			}
		}
	}

	_, _, err = h.client.gl.Discussions.CreateCommitDiscussion(projectID, commitSHA,
		&gl.CreateCommitDiscussionOptions{Body: &body}, gl.WithContext(ctx))
	return err
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *CommitCommentHandler) Close() error { return nil }
