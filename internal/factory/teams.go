package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/teams"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// TeamsFactory builds ActionHandlers from Teams instance configurations.
type TeamsFactory struct{}

// Build creates action handlers for a single Teams instance.
func (f *TeamsFactory) Build(inst config.TeamsInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	// Resolve webhook URL from volume mount
	webhookURLFile := ""
	webhookURLKey := ""
	if inst.Auth != nil {
		webhookURLFile = inst.Auth.WebhookURLFile
		webhookURLKey = inst.Auth.WebhookURLKey
	}
	webhookURL, err := secrets.ResolveOrInfer(webhookURLFile, "teams", inst.Name, "webhook_url", webhookURLKey, log)
	if err != nil {
		return nil, err
	}

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	handler, err := teams.New(teams.Config{
		WebhookURL:  webhookURL,
		Template:    inst.Template,
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
