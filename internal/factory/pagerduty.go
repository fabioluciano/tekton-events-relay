package factory //nolint:dupl // Factory structs are structurally similar by design

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/pagerduty"
)

// PagerDutyFactory builds ActionHandlers from PagerDuty instance configurations.
type PagerDutyFactory struct{}

// Build creates action handlers for a single PagerDuty instance.
func (f *PagerDutyFactory) Build(inst config.PagerDutyInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	integrationKeyFile := ""
	integrationKeyKey := ""
	if inst.Auth != nil {
		integrationKeyFile = inst.Auth.IntegrationKeyFile
		integrationKeyKey = inst.Auth.IntegrationKeyKey
	}
	integrationKey, err := resolveFileRefresher(integrationKeyFile, integrationKeyKey, "pagerduty", inst.Name, log)
	if err != nil {
		return nil, err
	}

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	handler := pagerduty.New(pagerduty.Config{
		IntegrationKey:       integrationKey,
		Severity:             inst.Severity,
		AcknowledgeOnRunning: inst.AcknowledgeOnRunning,
		HTTPClient:           httpClient,
		RetryPolicy:          retryPolicy,
	}, log)

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
