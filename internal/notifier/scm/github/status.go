package github

import (
	"context"
	"fmt"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// StatusReporter implements commit status updates for GitHub.
type StatusReporter struct {
	client *Client
}

// NewStatusReporter creates a new GitHub commit status reporter.
func NewStatusReporter(token, baseURL string, insecureSkipVerify bool) notifier.ActionHandler {
	return &StatusReporter{
		client: NewClient(token, baseURL, insecureSkipVerify),
	}
}

func (r *StatusReporter) Name() string                  { return "github" }
func (r *StatusReporter) Type() notifier.ActionType     { return notifier.ActionCommitStatus }

// Handle posts commit status to GitHub. Returns nil (skip) if provider doesn't match or required fields missing.
func (r *StatusReporter) Handle(ctx context.Context, e domain.Event) error {
	// Provider-match guard: skip if event not for GitHub
	if e.Provider != "github" {
		return nil // Skip silently, don't break dispatcher chain
	}

	// Validate required fields
	if e.Repo.Owner == "" || e.Repo.Name == "" || e.CommitSHA == "" {
		return nil // Skip if repo info missing (not an error)
	}

	url := fmt.Sprintf("%s/repos/%s/%s/statuses/%s",
		r.client.baseURL, e.Repo.Owner, e.Repo.Name, e.CommitSHA)

	if err := scm.Validate("github", "status_description", e.Description); err != nil {
		return err
	}
	if err := scm.Validate("github", "status_context", e.Context); err != nil {
		return err
	}

	payload := map[string]any{
		"state":       githubStateMap.Map(e.State, "error"),
		"target_url":  e.TargetURL,
		"description": e.Description,
		"context":     e.Context,
	}

	return r.client.Do(ctx, "POST", url, payload)
}

var githubStateMap = scm.StateMap{
	domain.StatePending:  "pending",
	domain.StateRunning:  "pending",
	domain.StateSuccess:  "success",
	domain.StateFailure:  "failure",
	domain.StateError:    "error",
	domain.StateCanceled: "error",
}
