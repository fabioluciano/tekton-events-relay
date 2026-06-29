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

// discussionMarkerAction labels the upsert marker so the relay can find the
// thread it owns across runs.
const discussionMarkerAction = "discussion_comment"

// DiscussionHandler opens a resolvable thread on a GitLab merge request while
// the pipeline is in a non-terminal state and resolves that same thread once
// the run succeeds.
//
// MR-only: GitLab issue discussions are not resolvable threads, so this handler
// gates on e.PRNumber (the MR IID) and is a no-op for any event without one.
type DiscussionHandler struct {
	client   *Client
	name     string
	template *template.Template
	log      *zap.Logger
}

// DiscussionConfig configures the MR discussion handler.
type DiscussionConfig struct {
	Client   *Client
	Name     string
	Template string
	Log      *zap.Logger
}

// NewDiscussionHandler creates a new GitLab merge request discussion handler.
func NewDiscussionHandler(cfg DiscussionConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("discussion_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}

	return &DiscussionHandler{
		client:   cfg.Client,
		name:     cfg.Name,
		template: tmpl,
		log:      log,
	}, nil
}

// Name returns the handler name.
func (h *DiscussionHandler) Name() string { return h.name }

// Type returns the action type.
func (h *DiscussionHandler) Type() notifier.ActionType { return notifier.ActionDiscussionComment }

// Handle opens or resolves a merge request thread depending on run state.
// The PR number annotation carries the MR IID. On success the relay-managed
// thread is resolved; in any other state the thread is created if absent so a
// failing run leaves an open thread for the reviewer.
func (h *DiscussionHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != h.name {
		return nil
	}

	// MR-only: issue discussions are not resolvable, so we require an MR IID.
	if e.PRNumber == nil {
		return nil
	}

	projectID, pErr := projectIdentifier(e)
	if pErr != nil {
		h.log.Warn("gitlab discussion skipped: project cannot be identified",
			zap.String("run", e.RunName),
			zap.Error(pErr))
		return nil //nolint:nilerr // intentional: drop event if project cannot be identified
	}

	mrIID := int64(*e.PRNumber)
	marker := scm.Marker(e.RunID, discussionMarkerAction)

	existing, err := h.findDiscussion(ctx, projectID, mrIID, marker)
	if err != nil {
		h.log.Warn("discussion: listing threads failed", zap.Error(err))
	}

	if e.State == domain.StateSuccess {
		return h.resolve(ctx, projectID, mrIID, existing, marker, e)
	}

	return h.open(ctx, projectID, mrIID, existing, marker, e)
}

// open ensures an unresolved relay-managed thread exists for the run. It is a
// no-op when the thread is already present so repeated non-terminal events do
// not spam the merge request.
func (h *DiscussionHandler) open(ctx context.Context, projectID string, mrIID int64, existing *gl.Discussion, marker string, e domain.Event) error {
	if existing != nil {
		return nil
	}

	body, err := h.renderBody(marker, e)
	if err != nil {
		return err
	}

	_, _, err = h.client.gl.Discussions.CreateMergeRequestDiscussion(projectID, mrIID,
		&gl.CreateMergeRequestDiscussionOptions{Body: &body}, gl.WithContext(ctx))
	return err
}

// resolve marks the relay-managed thread resolved. When the thread is absent
// (e.g. the relay only observed the terminal event) it is created first so the
// success state still produces a resolved thread.
func (h *DiscussionHandler) resolve(ctx context.Context, projectID string, mrIID int64, existing *gl.Discussion, marker string, e domain.Event) error {
	if existing == nil {
		body, err := h.renderBody(marker, e)
		if err != nil {
			return err
		}
		created, _, cErr := h.client.gl.Discussions.CreateMergeRequestDiscussion(projectID, mrIID,
			&gl.CreateMergeRequestDiscussionOptions{Body: &body}, gl.WithContext(ctx))
		if cErr != nil {
			return cErr
		}
		existing = created
	}

	resolved := true
	_, _, err := h.client.gl.Discussions.ResolveMergeRequestDiscussion(projectID, mrIID, existing.ID,
		&gl.ResolveMergeRequestDiscussionOptions{Resolved: &resolved}, gl.WithContext(ctx))
	return err
}

// renderBody renders the configured template and prepends the upsert marker so
// the thread can be located on later events.
func (h *DiscussionHandler) renderBody(marker string, e domain.Event) (string, error) {
	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	body = scm.WithMarker(marker, body)
	if err := scm.Validate(h.name, "comment_body", body); err != nil {
		return "", err
	}
	return body, nil
}

// findDiscussion scans the MR threads for the relay-managed thread carrying the
// marker on its first note. Returns nil (no error) when none matches.
func (h *DiscussionHandler) findDiscussion(ctx context.Context, projectID string, mrIID int64, marker string) (*gl.Discussion, error) {
	discussions, _, err := h.client.gl.Discussions.ListMergeRequestDiscussions(projectID, mrIID,
		&gl.ListMergeRequestDiscussionsOptions{ListOptions: gl.ListOptions{PerPage: 100}},
		gl.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	for _, d := range discussions {
		if d == nil {
			continue
		}
		for _, n := range d.Notes {
			if n != nil && scm.HasMarker(n.Body, marker) {
				return d, nil
			}
		}
	}
	return nil, nil //nolint:nilnil // not-found is a valid, non-error result here
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *DiscussionHandler) Close() error { return nil }
