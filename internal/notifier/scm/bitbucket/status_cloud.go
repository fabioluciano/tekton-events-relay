package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// CloudStatusReporter implements commit status updates for Bitbucket Cloud.
type CloudStatusReporter struct {
	name   string
	client *CloudClient
}

// NewCloudStatusReporter creates a new Bitbucket Cloud commit status reporter
// using basic auth credentials.
func NewCloudStatusReporter(name, username, appPassword, baseURL string, insecureSkipVerify bool, log *zap.Logger) notifier.ActionHandler {
	return &CloudStatusReporter{
		name:   name,
		client: NewCloudClient(username, appPassword, baseURL, insecureSkipVerify, false, log),
	}
}

// NewCloudStatusReporterWithClient creates a new Bitbucket Cloud commit status
// reporter using a pre-built CloudClient. Use this for OAuth2 auth where the
// client resolves tokens per-request via an AuthFunc.
func NewCloudStatusReporterWithClient(name string, client *CloudClient) notifier.ActionHandler {
	return &CloudStatusReporter{name: name, client: client}
}

// Name returns the handler name.
func (r *CloudStatusReporter) Name() string { return r.name }

// Provider returns the provider type identifier.
func (r *CloudStatusReporter) Provider() string { return providerCloud }

// Type returns the action type.
func (r *CloudStatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts commit status to Bitbucket Cloud.
func (r *CloudStatusReporter) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerCloud {
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
		strings.TrimRight(r.client.BaseURL, "/"), ws, e.Repo.Name, e.CommitSHA)

	key := e.Context
	if key == "" {
		key = "tekton-" + e.RunName
	}

	if err := scm.Validate(providerCloud, "status_context", key); err != nil {
		return err
	}
	if err := scm.Validate(providerCloud, "status_context", e.Context); err != nil {
		return err
	}
	if err := scm.Validate(providerCloud, "status_description", e.Description); err != nil {
		return err
	}

	payload := map[string]string{
		"key":         key,
		"state":       bitbucketCloudStateMap.Map(e.State, stateInProgress),
		"name":        e.Context,
		"description": e.Description,
		"url":         e.TargetURL,
	}

	return r.client.Do(ctx, "POST", url, payload)
}

var bitbucketCloudStateMap = scm.StateMap{
	domain.StatePending:  stateInProgress,
	domain.StateRunning:  stateInProgress,
	domain.StateSuccess:  stateSuccessful,
	domain.StateFailure:  stateFailed,
	domain.StateError:    stateFailed,
	domain.StateCanceled: stateStopped,
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (r *CloudStatusReporter) Close() error { return nil }
