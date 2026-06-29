package factory //nolint:dupl // Factory structs are structurally similar by design

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/opsgenie"
)

// OpsgenieFactory builds ActionHandlers from Opsgenie instance configurations.
type OpsgenieFactory struct{}

// Build creates action handlers for a single Opsgenie instance.
func (f *OpsgenieFactory) Build(inst config.OpsgenieInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	apiKeyFile := ""
	apiKeyKey := ""
	if inst.Auth != nil {
		apiKeyFile = inst.Auth.APIKeyFile
		apiKeyKey = inst.Auth.APIKeyKey
	}
	apiKey, err := resolveFileRefresher(apiKeyFile, apiKeyKey, "opsgenie", inst.Name, log)
	if err != nil {
		return nil, err
	}

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	handler := opsgenie.New(opsgenie.Config{
		APIKey:      apiKey,
		TeamName:    inst.TeamName,
		Priority:    inst.Priority,
		HTTPClient:  httpClient,
		RetryPolicy: retryPolicy,
	}, log)

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
