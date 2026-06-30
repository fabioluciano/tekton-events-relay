package gitlab

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	gl "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// incidentLabelPrefix is the label prefix used for deduplication.
// Each incident gets a label "tekton-relay:{RunID}" so we can find
// and close it later without storing extra state.
const incidentLabelPrefix = "tekton-relay:"

// IncidentCreateHandler creates GitLab incidents on failure/error and
// closes them on success. Uses labels for deduplication — searching
// before creating avoids duplicate incidents for the same run.
//
// Fallback: if the GitLab instance does not support issue_type=incident
// (< 14.0), the handler retries with issue_type=issue.
type IncidentCreateHandler struct {
	client   *Client
	name     string
	template *template.Template
	log      *zap.Logger
}

// IncidentCreateConfig configures the incident create handler.
type IncidentCreateConfig struct {
	Client   *Client
	Name     string
	Template string
	Log      *zap.Logger
}

// NewIncidentCreateHandler creates a new GitLab incident create handler.
func NewIncidentCreateHandler(cfg IncidentCreateConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("incident_create", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}

	return &IncidentCreateHandler{
		client:   cfg.Client,
		name:     cfg.Name,
		template: tmpl,
		log:      log,
	}, nil
}

// Name returns the handler name.
func (h *IncidentCreateHandler) Name() string { return h.name }

// Provider returns the provider type identifier.
func (h *IncidentCreateHandler) Provider() string { return providerGitLab }

// Type returns the action type.
func (h *IncidentCreateHandler) Type() notifier.ActionType { return notifier.ActionIncidentCreate }

// Handle creates or closes GitLab incidents based on event state.
//   - failure/error: search for existing incident with dedup label; create if not found
//   - success: search for existing incident with dedup label; close if found
//   - other states: no-op
func (h *IncidentCreateHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerGitLab {
		return nil
	}

	projectID, pErr := projectIdentifier(e)
	if pErr != nil {
		h.log.Warn("gitlab incident_create skipped: project cannot be identified",
			zap.String("run", e.RunName), zap.Error(pErr))
		return nil //nolint:nilerr // intentional: drop event if project cannot be identified
	}

	dedupLabel := incidentLabelPrefix + e.RunID

	switch e.State {
	case domain.StateFailure, domain.StateError:
		return h.createIncident(ctx, e, projectID, dedupLabel)
	case domain.StateSuccess:
		return h.closeIncident(ctx, e, projectID, dedupLabel)
	default:
		return nil // pending, running, canceled — no action
	}
}

// createIncident searches for an existing incident with the dedup label.
// If none exists, creates a new issue with issue_type=incident. Falls back
// to issue_type=issue for GitLab < 14.0.
func (h *IncidentCreateHandler) createIncident(ctx context.Context, e domain.Event, projectID, dedupLabel string) error {
	existing, err := h.findIncidentsByLabel(ctx, projectID, dedupLabel)
	if err != nil {
		return fmt.Errorf("search existing incident: %w", err)
	}
	if len(existing) > 0 {
		h.log.Debug("incident already exists, skipping create",
			zap.String("run", e.RunName),
			zap.Int64("iid", existing[0].IID))
		return nil
	}

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	title := fmt.Sprintf("Pipeline failed: %s", e.RunName)
	if e.State == domain.StateError {
		title = fmt.Sprintf("Pipeline error: %s", e.RunName)
	}

	labels := gl.LabelOptions([]string{dedupLabel})

	// Try creating with issue_type=incident first.
	issueType := "incident"
	opts := &gl.CreateIssueOptions{
		Title:       gl.Ptr(title),
		Description: gl.Ptr(body),
		Labels:      &labels,
		IssueType:   &issueType,
	}

	issue, _, createErr := h.client.gl.Issues.CreateIssue(projectID, opts, gl.WithContext(ctx))
	if createErr != nil {
		// Fallback to issue_type=issue for GitLab < 14.0.
		if isIssueTypeUnsupported(createErr) {
			h.log.Debug("issue_type=incident unsupported, falling back to issue",
				zap.String("run", e.RunName),
				zap.Error(createErr))
			issueType = "issue"
			opts.IssueType = &issueType
			issue, _, createErr = h.client.gl.Issues.CreateIssue(projectID, opts, gl.WithContext(ctx))
		}
		if createErr != nil {
			return fmt.Errorf("create incident: %w", createErr)
		}
	}

	h.log.Info("incident created",
		zap.String("run", e.RunName),
		zap.Int64("iid", issue.IID),
		zap.String("state", string(e.State)))
	return nil
}

// closeIncident finds the incident by dedup label and closes it.
// If no incident is found (e.g. the create was skipped by a when expression),
// this is a silent no-op.
func (h *IncidentCreateHandler) closeIncident(ctx context.Context, e domain.Event, projectID, dedupLabel string) error {
	existing, err := h.findIncidentsByLabel(ctx, projectID, dedupLabel)
	if err != nil {
		return fmt.Errorf("search incident to close: %w", err)
	}
	if len(existing) == 0 {
		h.log.Debug("no incident found to close",
			zap.String("run", e.RunName))
		return nil
	}
	incident := existing[0]

	if incident.State == "closed" {
		h.log.Debug("incident already closed",
			zap.String("run", e.RunName),
			zap.Int64("iid", incident.IID))
		return nil
	}

	stateEvent := "close"
	_, _, err = h.client.gl.Issues.UpdateIssue(projectID, incident.IID,
		&gl.UpdateIssueOptions{StateEvent: &stateEvent},
		gl.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("close incident: %w", err)
	}

	h.log.Info("incident closed",
		zap.String("run", e.RunName),
		zap.Int64("iid", incident.IID))
	return nil
}

func (h *IncidentCreateHandler) findIncidentsByLabel(ctx context.Context, projectID, label string) ([]*gl.Issue, error) {
	labels := gl.LabelOptions([]string{label})
	state := "opened"
	opts := &gl.ListProjectIssuesOptions{
		Labels: &labels,
		State:  &state,
		ListOptions: gl.ListOptions{
			PerPage: 1,
		},
	}

	issues, _, err := h.client.gl.Issues.ListProjectIssues(projectID, opts, gl.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	return issues, nil
}

// isIssueTypeUnsupported checks if the error indicates that the issue_type
// field is not supported (GitLab < 14.0).
func isIssueTypeUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "issue_type") ||
		strings.Contains(msg, "is invalid") ||
		strings.Contains(msg, "400")
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *IncidentCreateHandler) Close() error { return nil }
