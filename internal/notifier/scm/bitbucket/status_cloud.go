package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// CloudStatusReporter implements commit status updates for Bitbucket Cloud.
type CloudStatusReporter struct {
	client *CloudClient
}

// NewCloudStatusReporter creates a new Bitbucket Cloud commit status reporter.
func NewCloudStatusReporter(username, appPassword, baseURL string, insecureSkipVerify bool) notifier.ActionHandler {
	return &CloudStatusReporter{
		client: NewCloudClient(username, appPassword, baseURL, insecureSkipVerify),
	}
}

func (r *CloudStatusReporter) Name() string              { return "bitbucket-cloud" }
func (r *CloudStatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts commit status to Bitbucket Cloud.
func (r *CloudStatusReporter) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != "bitbucket-cloud" {
		return nil
	}

	ws := e.Repo.Workspace
	if ws == "" {
		ws = e.Repo.Owner
	}
	if ws == "" || e.Repo.Name == "" || e.CommitSHA == "" {
		return nil
	}

	url := fmt.Sprintf("%s/2.0/repositories/%s/%s/commit/%s/statuses/build",
		strings.TrimRight(r.client.baseURL, "/"), ws, e.Repo.Name, e.CommitSHA)

	key := e.Context
	if key == "" {
		key = "tekton-" + e.RunName
	}

	if err := scm.Validate("bitbucket-cloud", "status_context", key); err != nil {
		return err
	}
	if err := scm.Validate("bitbucket-cloud", "status_context", e.Context); err != nil {
		return err
	}
	if err := scm.Validate("bitbucket-cloud", "status_description", e.Description); err != nil {
		return err
	}

	payload := map[string]string{
		"key":         key,
		"state":       bitbucketCloudStateMap.Map(e.State, "INPROGRESS"),
		"name":        e.Context,
		"description": e.Description,
		"url":         e.TargetURL,
	}

	return r.client.Do(ctx, "POST", url, payload)
}

var bitbucketCloudStateMap = scm.StateMap{
	domain.StatePending:  "INPROGRESS",
	domain.StateRunning:  "INPROGRESS",
	domain.StateSuccess:  "SUCCESSFUL",
	domain.StateFailure:  "FAILED",
	domain.StateError:    "FAILED",
	domain.StateCanceled: "STOPPED",
}
