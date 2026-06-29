package factory //nolint:dupl // Factory structs are structurally similar by design

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/honeycomb"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
)

// HoneycombFactory builds ActionHandlers from Honeycomb instance configurations.
type HoneycombFactory struct{}

// Build creates action handlers for a single Honeycomb instance.
func (f *HoneycombFactory) Build(inst config.HoneycombInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	apiKeyFile := ""
	apiKeyKey := ""
	if inst.Auth != nil {
		apiKeyFile = inst.Auth.APIKeyFile
		apiKeyKey = inst.Auth.APIKeyKey
	}
	apiKey, err := resolveFileRefresher(apiKeyFile, apiKeyKey, "honeycomb", inst.Name, log)
	if err != nil {
		return nil, err
	}

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	handler := honeycomb.New(honeycomb.Config{
		APIKey:      apiKey,
		Dataset:     inst.Dataset,
		HTTPClient:  httpClient,
		RetryPolicy: retryPolicy,
	}, log)

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
