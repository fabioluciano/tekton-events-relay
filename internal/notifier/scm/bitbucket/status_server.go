package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// ServerStatusReporter implements commit status updates for Bitbucket Server.
type ServerStatusReporter struct {
	client *ServerClient
}

// NewServerStatusReporter creates a new Bitbucket Server commit status reporter.
func NewServerStatusReporter(token, baseURL string, insecureSkipVerify bool) notifier.ActionHandler {
	return &ServerStatusReporter{
		client: NewServerClient(token, baseURL, insecureSkipVerify),
	}
}

func (r *ServerStatusReporter) Name() string              { return "bitbucket-server" }
func (r *ServerStatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts commit status to Bitbucket Server.
func (r *ServerStatusReporter) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != "bitbucket-server" {
		return nil
	}

	if e.CommitSHA == "" || r.client.baseURL == "" {
		return nil
	}

	url := fmt.Sprintf("%s/rest/build-status/1.0/commits/%s",
		strings.TrimRight(r.client.baseURL, "/"), e.CommitSHA)

	key := e.Context
	if key == "" {
		key = "tekton-" + e.RunName
	}

	if err := scm.Validate("bitbucket-server", "status_context", key); err != nil {
		return err
	}
	if err := scm.Validate("bitbucket-server", "status_context", e.Context); err != nil {
		return err
	}
	if err := scm.Validate("bitbucket-server", "status_description", e.Description); err != nil {
		return err
	}

	payload := map[string]string{
		"state":       bitbucketServerStateMap.Map(e.State, "INPROGRESS"),
		"key":         key,
		"name":        e.Context,
		"url":         e.TargetURL,
		"description": e.Description,
	}

	if e.Repo.Project != "" {
		if err := scm.Validate("bitbucket-server", "status_context", e.Repo.Project); err != nil {
			return err
		}
		payload["parent"] = e.Repo.Project
	}

	return r.client.Do(ctx, "POST", url, payload)
}

var bitbucketServerStateMap = scm.StateMap{
	domain.StatePending:  "INPROGRESS",
	domain.StateRunning:  "INPROGRESS",
	domain.StateSuccess:  "SUCCESSFUL",
	domain.StateFailure:  "FAILED",
	domain.StateError:    "FAILED",
	domain.StateCanceled: "FAILED",
}
