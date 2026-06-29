package factory //nolint:dupl // Factory structs are structurally similar by design

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/newrelic"
)

// NewRelicFactory builds ActionHandlers from New Relic instance configurations.
type NewRelicFactory struct{}

// Build creates action handlers for a single New Relic instance.
func (f *NewRelicFactory) Build(inst config.NewRelicInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	apiKeyFile := ""
	apiKeyKey := ""
	if inst.Auth != nil {
		apiKeyFile = inst.Auth.APIKeyFile
		apiKeyKey = inst.Auth.APIKeyKey
	}
	apiKey, err := resolveFileRefresher(apiKeyFile, apiKeyKey, "newrelic", inst.Name, log)
	if err != nil {
		return nil, err
	}

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	handler := newrelic.New(newrelic.Config{
		APIKey:      apiKey,
		AccountID:   inst.AccountID,
		HTTPClient:  httpClient,
		RetryPolicy: retryPolicy,
	}, log)

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
