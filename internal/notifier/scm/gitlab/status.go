package gitlab

import (
	"context"
	"fmt"

	gl "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// StatusReporter implements commit status updates for GitLab.
type StatusReporter struct {
	client *Client
	name   string
	log    *zap.Logger
}

// NewStatusReporter creates a new GitLab commit status reporter.
func NewStatusReporter(client *Client, name string, log *zap.Logger) (notifier.ActionHandler, error) {
	if log == nil {
		log = zap.NewNop()
	}
	return &StatusReporter{
		client: client,
		name:   name,
		log:    log,
	}, nil
}

// Name returns the handler name.
func (r *StatusReporter) Name() string { return r.name }

// Provider returns the provider type identifier.
func (r *StatusReporter) Provider() string { return providerGitLab }

// Type returns the action type.
func (r *StatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts commit status to GitLab.
func (r *StatusReporter) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerGitLab {
		return nil
	}

	if e.CommitSHA == "" {
		r.log.Warn("gitlab status skipped: missing commit SHA",
			zap.String("run", e.RunName))
		return nil
	}

	projectID, pErr := projectIdentifier(e)
	if pErr != nil {
		r.log.Warn("gitlab status skipped: project cannot be identified",
			zap.String("run", e.RunName),
			zap.Error(pErr))
		return nil //nolint:nilerr // intentional: drop event if project cannot be identified
	}

	if err := scm.Validate(r.name, "status_context", e.Context); err != nil {
		return fmt.Errorf("validate status context: %w", err)
	}
	if err := scm.Validate(r.name, "status_description", e.Description); err != nil {
		return fmt.Errorf("validate status description: %w", err)
	}

	state := mapStateToGitLab(e.State)
	opts := &gl.SetCommitStatusOptions{
		State:       state,
		Name:        &e.Context,
		Description: &e.Description,
	}
	if e.TargetURL != "" {
		opts.TargetURL = &e.TargetURL
	}

	_, _, err := r.client.gl.Commits.SetCommitStatus(projectID, e.CommitSHA, opts, gl.WithContext(ctx))
	return err
}

// mapStateToGitLab converts domain state to GitLab BuildStateValue.
func mapStateToGitLab(state domain.State) gl.BuildStateValue {
	switch state {
	case domain.StatePending:
		return gl.Pending
	case domain.StateRunning:
		return gl.Running
	case domain.StateSuccess:
		return gl.Success
	case domain.StateFailure, domain.StateError:
		return gl.Failed
	case domain.StateCanceled:
		return gl.Canceled
	default:
		return gl.Pending
	}
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (r *StatusReporter) Close() error { return nil }
