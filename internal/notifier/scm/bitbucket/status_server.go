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

// ServerStatusReporter implements commit status updates for Bitbucket Server.
type ServerStatusReporter struct {
	client *ServerClient
}

// NewServerStatusReporter creates a new Bitbucket Server commit status reporter.
func NewServerStatusReporter(token, baseURL string, insecureSkipVerify bool, log *zap.Logger) notifier.ActionHandler {
	return &ServerStatusReporter{
		client: NewServerClient(token, baseURL, insecureSkipVerify, false, log),
	}
}

// Name returns the handler name.
func (r *ServerStatusReporter) Name() string { return providerServer }

// Type returns the action type.
func (r *ServerStatusReporter) Type() notifier.ActionType { return notifier.ActionCommitStatus }

// Handle posts commit status to Bitbucket Server.
func (r *ServerStatusReporter) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerServer {
		return nil
	}

	if e.CommitSHA == "" || r.client.BaseURL == "" {
		return nil
	}

	url := fmt.Sprintf("%s/rest/build-status/1.0/commits/%s",
		strings.TrimRight(r.client.BaseURL, "/"), e.CommitSHA)

	key := e.Context
	if key == "" {
		key = "tekton-" + e.RunName
	}

	if err := scm.Validate(providerServer, "status_context", key); err != nil {
		return err
	}
	if err := scm.Validate(providerServer, "status_context", e.Context); err != nil {
		return err
	}
	if err := scm.Validate(providerServer, "status_description", e.Description); err != nil {
		return err
	}

	payload := map[string]string{
		"state":       bitbucketServerStateMap.Map(e.State, stateInProgress),
		"key":         key,
		"name":        e.Context,
		"url":         e.TargetURL,
		"description": e.Description,
	}

	if e.Repo.Project != "" {
		if err := scm.Validate(providerServer, "status_context", e.Repo.Project); err != nil {
			return fmt.Errorf("validate repo project: %w", err)
		}
		payload["parent"] = e.Repo.Project
	}

	return r.client.Do(ctx, "POST", url, payload)
}

var bitbucketServerStateMap = scm.StateMap{
	domain.StatePending:  stateInProgress,
	domain.StateRunning:  stateInProgress,
	domain.StateSuccess:  stateSuccessful,
	domain.StateFailure:  stateFailed,
	domain.StateError:    stateFailed,
	domain.StateCanceled: stateFailed,
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (r *ServerStatusReporter) Close() error { return nil }
