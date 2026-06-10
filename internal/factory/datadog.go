package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/datadog"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// DatadogFactory builds ActionHandlers from Datadog instance configurations.
type DatadogFactory struct{}

// Build creates action handlers for a single Datadog instance.
func (f *DatadogFactory) Build(inst config.DatadogInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	// Resolve API key from volume mount
	apiKeyFile := ""
	apiKeyKey := ""
	if inst.Auth != nil {
		apiKeyFile = inst.Auth.APIKeyFile
		apiKeyKey = inst.Auth.APIKeyKey
	}
	apiKey, err := secrets.ResolveOrInfer(apiKeyFile, "datadog", inst.Name, "api_key", apiKeyKey, log)
	if err != nil {
		return nil, err
	}

	handler := datadog.New(datadog.Config{
		APIKey: apiKey,
		Site:   inst.Site,
		Tags:   inst.Tags,
	}, log)

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
