package gitea

import (
	"context"

	giteaSDK "code.gitea.io/sdk/gitea"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// StatusReporter implements commit status updates for Gitea.
type StatusReporter struct {
	client *Client
	name   string
	log    *zap.Logger
}

// NewStatusReporter creates a new Gitea commit status reporter.
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
func (r *StatusReporter) Provider() string { return providerGitea }

// Type returns the action type.
func (r *StatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts commit status to Gitea.
func (r *StatusReporter) Handle(_ context.Context, e domain.Event) error {
	if e.Provider != providerGitea {
		return nil
	}

	if e.Repo.Owner == "" || e.Repo.Name == "" || e.CommitSHA == "" {
		r.log.Warn("gitea status skipped: missing repo owner/name or commit SHA",
			zap.String("run", e.RunName),
			zap.String("owner", e.Repo.Owner),
			zap.String("repo", e.Repo.Name))
		return nil
	}

	if err := scm.Validate(r.name, "status_context", e.Context); err != nil {
		return err
	}
	if err := scm.Validate(r.name, "status_description", e.Description); err != nil {
		return err
	}

	state := giteaSDK.StatusState(giteaStateMap.Map(e.State, statePending))
	opts := giteaSDK.CreateStatusOption{
		State:       state,
		Context:     e.Context,
		Description: e.Description,
		TargetURL:   e.TargetURL,
	}

	_, _, err := r.client.sdk.CreateStatus(e.Repo.Owner, e.Repo.Name, e.CommitSHA, opts)
	return err
}

var giteaStateMap = scm.StateMap{
	domain.StatePending:  statePending,
	domain.StateRunning:  statePending,
	domain.StateSuccess:  "success",
	domain.StateFailure:  "failure",
	domain.StateError:    "error",
	domain.StateCanceled: "error",
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (r *StatusReporter) Close() error { return nil }
