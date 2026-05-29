package gitlab

import (
	"context"
	"fmt"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// StatusReporter implements commit status updates for GitLab.
type StatusReporter struct {
	client *Client
	name   string
}

// NewStatusReporter creates a new GitLab commit status reporter.
func NewStatusReporter(token, baseURL, name string, insecureSkipVerify bool) notifier.ActionHandler {
	return &StatusReporter{
		client: NewClient(token, baseURL, insecureSkipVerify),
		name:   name,
	}
}

func (r *StatusReporter) Name() string              { return r.name }
func (r *StatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts commit status to GitLab.
func (r *StatusReporter) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != r.name {
		return nil
	}

	if e.CommitSHA == "" {
		return nil
	}

	projectID, err := projectIdentifier(e)
	if err != nil {
		return nil
	}

	url := fmt.Sprintf("%s/projects/%s/statuses/%s",
		strings.TrimRight(r.client.baseURL, "/"), projectID, e.CommitSHA)

	if err := scm.Validate(r.name, "status_context", e.Context); err != nil {
		return err
	}
	if err := scm.Validate(r.name, "status_description", e.Description); err != nil {
		return err
	}

	payload := map[string]any{
		"state":       gitlabStateMap.Map(e.State, "pending"),
		"name":        e.Context,
		"description": e.Description,
	}
	if e.TargetURL != "" {
		payload["target_url"] = e.TargetURL
	}

	return r.client.Do(ctx, "POST", url, payload)
}

var gitlabStateMap = scm.StateMap{
	domain.StatePending:  "pending",
	domain.StateRunning:  "running",
	domain.StateSuccess:  "success",
	domain.StateFailure:  "failed",
	domain.StateError:    "failed",
	domain.StateCanceled: "canceled",
}
