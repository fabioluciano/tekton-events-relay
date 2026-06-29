package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/mattermost"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// MattermostFactory builds ActionHandlers from Mattermost instance configurations.
type MattermostFactory struct{}

// Build creates action handlers for a single Mattermost instance.
func (f *MattermostFactory) Build(inst config.MattermostInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	var mmCfg mattermost.Config
	if inst.Auth != nil && inst.Auth.BotToken != nil {
		// Bot token mode — per-request token (rotation-safe), never a static string.
		refresher, err := resolveFileRefresher(inst.Auth.BotToken.TokenFile, inst.Auth.BotToken.TokenKey, "mattermost", inst.Name, log)
		if err != nil {
			return nil, err
		}
		mmCfg = mattermost.Config{
			Token:     refresher,
			BaseURL:   inst.BaseURL,
			ChannelID: inst.Auth.BotToken.ChannelID,
			Channel:   inst.Channel,
			Username:  inst.Username,
			IconURL:   inst.IconURL,
			Template:  inst.Template,
		}
	} else {
		// Webhook mode
		webhookURLFile := ""
		webhookURLKey := ""
		if inst.Auth != nil {
			webhookURLFile = inst.Auth.WebhookURLFile
			webhookURLKey = inst.Auth.WebhookURLKey
		}
		webhookURL, err := secrets.ResolveOrInfer(webhookURLFile, "mattermost", inst.Name, "webhook_url", webhookURLKey, log)
		if err != nil {
			return nil, err
		}
		mmCfg = mattermost.Config{
			WebhookURL:  webhookURL,
			Channel:     inst.Channel,
			Username:    inst.Username,
			IconURL:     inst.IconURL,
			Template:    inst.Template,
			HTTPClient:  httpClient,
			RetryPolicy: retryPolicy,
		}
	}

	handler, err := mattermost.New(mmCfg, log)
	if err != nil {
		return nil, err
	}

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
