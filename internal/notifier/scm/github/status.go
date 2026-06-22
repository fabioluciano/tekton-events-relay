package github

import (
	"context"

	gh "github.com/google/go-github/v68/github"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// StatusReporter implements commit status updates for GitHub.
type StatusReporter struct {
	client HTTPDoer
	log    *zap.Logger
}

// NewStatusReporter creates a new GitHub commit status reporter.
func NewStatusReporter(client HTTPDoer, log *zap.Logger) notifier.ActionHandler {
	return &StatusReporter{
		client: client,
		log:    log,
	}
}

// Name returns the handler name.
func (r *StatusReporter) Name() string { return providerGitHub }

// Type returns the action type.
func (r *StatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts commit status to GitHub using the typed go-github SDK.
// Returns nil (skip) if provider doesn't match or required fields missing.
func (r *StatusReporter) Handle(ctx context.Context, e domain.Event) error {
	// Provider-match guard: skip if event not for GitHub
	if e.Provider != providerGitHub {
		r.log.Debug("commit status skipped: provider mismatch",
			zap.String("provider", e.Provider),
			zap.String("namespace", e.Namespace),
			zap.String("taskrun", e.RunName))
		return nil
	}

	// Validate required fields
	if e.CommitSHA == "" {
		r.log.Info("commit status NOT sent: missing scm.commit-sha annotation",
			zap.String("namespace", e.Namespace),
			zap.String("taskrun", e.RunName),
			zap.String("note", "expected for issue/discussion triggers"))
		return nil
	}

	if e.Repo.Owner == "" || e.Repo.Name == "" {
		r.log.Warn("commit status NOT sent: missing scm.repo-owner or scm.repo-name annotation",
			zap.String("namespace", e.Namespace),
			zap.String("taskrun", e.RunName),
			zap.String("repo_owner", e.Repo.Owner),
			zap.String("repo_name", e.Repo.Name))
		return nil
	}

	if err := scm.Validate(providerGitHub, "status_description", e.Description); err != nil {
		return err
	}
	if err := scm.Validate(providerGitHub, "status_context", e.Context); err != nil {
		return err
	}

	status := &gh.RepoStatus{
		State:   gh.Ptr(githubStateMap.Map(e.State, stateError)),
		Context: gh.Ptr(e.Context),
	}
	if e.Description != "" {
		status.Description = gh.Ptr(e.Description)
	}
	if e.TargetURL != "" {
		status.TargetURL = gh.Ptr(e.TargetURL)
	}

	_, _, err := r.client.GH().Repositories.CreateStatus(ctx, e.Repo.Owner, e.Repo.Name, e.CommitSHA, status)
	return err
}

var githubStateMap = scm.StateMap{
	domain.StatePending:  "pending",
	domain.StateRunning:  "pending",
	domain.StateSuccess:  stateSuccess,
	domain.StateFailure:  stateFailure,
	domain.StateError:    stateError,
	domain.StateCanceled: stateError,
}
