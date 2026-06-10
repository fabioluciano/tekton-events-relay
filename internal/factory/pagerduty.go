package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/pagerduty"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// PagerDutyFactory builds ActionHandlers from PagerDuty instance configurations.
type PagerDutyFactory struct{}

// Build creates action handlers for a single PagerDuty instance.
func (f *PagerDutyFactory) Build(inst config.PagerDutyInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	// Resolve integration key from volume mount
	integrationKeyFile := ""
	integrationKeyKey := ""
	if inst.Auth != nil {
		integrationKeyFile = inst.Auth.IntegrationKeyFile
		integrationKeyKey = inst.Auth.IntegrationKeyKey
	}
	integrationKey, err := secrets.ResolveOrInfer(integrationKeyFile, "pagerduty", inst.Name, "integration_key", integrationKeyKey, log)
	if err != nil {
		return nil, err
	}

	handler := pagerduty.New(pagerduty.Config{
		IntegrationKey: integrationKey,
		Severity:       inst.Severity,
	}, log)

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
