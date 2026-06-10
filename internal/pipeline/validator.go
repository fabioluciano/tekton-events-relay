package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// Validator checks required fields before processing.
type Validator struct {
	BaseHandler
}

// NewValidator creates a new Validator.
func NewValidator() *Validator { return &Validator{} }

// Handle validates that the event envelope has all required fields.
func (v *Validator) Handle(ctx context.Context, env *event.Envelope) error {
	if env == nil {
		return errors.New("nil envelope")
	}
	if env.CloudEventID == "" {
		return errors.New("missing Ce-Id")
	}
	r := env.Report

	// EventListener events don't have SCM context (no provider/commit)
	if r.Resource != "eventlistener" {
		if r.Provider == "" {
			return errors.New("missing tekton.dev/tekton-events-relay.scm.provider annotation")
		}
		// CommitSHA is optional - issue/discussion triggers may not have it.
		// Handlers that require SHA (commit_status, check_run, deployment_status) validate individually.
	}

	if r.RunName == "" {
		return fmt.Errorf("missing RunName for %s", env.CloudEventType)
	}
	return v.Next(ctx, env)
}
