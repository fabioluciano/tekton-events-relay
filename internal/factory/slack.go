package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/slack"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// SlackFactory builds ActionHandlers from Slack instance configurations.
type SlackFactory struct{}

// Build creates action handlers for a single Slack instance.
func (f *SlackFactory) Build(inst config.SlackInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	var slackCfg slack.Config
	if inst.Auth != nil && inst.Auth.BotToken != nil {
		// Bot token mode
		token, err := secrets.ResolveOrInfer(inst.Auth.BotToken.TokenFile, "slack", inst.Name, "token", inst.Auth.BotToken.TokenKey, log)
		if err != nil {
			return nil, err
		}
		slackCfg = slack.Config{
			BotToken:  token,
			ChannelID: inst.Auth.BotToken.ChannelID,
			Channel:   inst.Channel,
			Username:  inst.Username,
			IconEmoji: inst.IconEmoji,
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
		webhookURL, err := secrets.ResolveOrInfer(webhookURLFile, "slack", inst.Name, "webhook_url", webhookURLKey, log)
		if err != nil {
			return nil, err
		}
		slackCfg = slack.Config{
			WebhookURL: webhookURL,
			Channel:    inst.Channel,
			Username:   inst.Username,
			IconEmoji:  inst.IconEmoji,
			Template:   inst.Template,
		}
	}

	handler, err := slack.New(slackCfg, log)
	if err != nil {
		return nil, err
	}

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
