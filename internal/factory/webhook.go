package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/webhook"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// WebhookFactory builds ActionHandlers from Webhook instance configurations.
type WebhookFactory struct{}

// Build creates action handlers for a single Webhook instance.
func (f *WebhookFactory) Build(inst config.WebhookInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	// Resolve URL from volume mount
	url, err := secrets.ResolveOrInfer(inst.URLFile, "webhook", inst.Name, "url", inst.URLKey, log)
	if err != nil {
		return nil, err
	}

	// Resolve auth secrets if auth is configured
	var auth *webhook.ResolvedAuth
	if inst.Auth != nil {
		resolvedAuth, err := f.resolveAuthSecrets(inst.Name, inst.Auth, log)
		if err != nil {
			return nil, err
		}
		auth = resolvedAuth
	}

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	handler, err := webhook.New(webhook.Config{
		URL:         url,
		Auth:        auth,
		Transform:   inst.Transform,
		Headers:     inst.Headers,
		HTTPClient:  httpClient,
		RetryPolicy: retryPolicy,
	}, log)
	if err != nil {
		return nil, err
	}

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
