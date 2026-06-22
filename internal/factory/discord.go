package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/discord"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/msgstore"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// DiscordFactory builds ActionHandlers from Discord instance configurations.
type DiscordFactory struct{}

// Build creates action handlers for a single Discord instance.
func (f *DiscordFactory) Build(inst config.DiscordInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	var discordCfg discord.Config
	if inst.Auth != nil && inst.Auth.BotToken != nil {
		// Bot token mode — per-request token (rotation-safe), never a static string.
		refresher, err := resolveFileRefresher(inst.Auth.BotToken.TokenFile, inst.Auth.BotToken.TokenKey, "discord", inst.Name, log)
		if err != nil {
			return nil, err
		}
		discordCfg = discord.Config{
			BotToken:     refresher,
			ChannelID:    inst.Auth.BotToken.ChannelID,
			Username:     inst.Username,
			Template:     inst.Template,
			Mode:         inst.Mode,
			MessageStore: msgstore.NewMemoryStore(0, 0),
		}
	} else {
		// Webhook mode
		webhookURLFile := ""
		webhookURLKey := ""
		if inst.Auth != nil {
			webhookURLFile = inst.Auth.WebhookURLFile
			webhookURLKey = inst.Auth.WebhookURLKey
		}
		webhookURL, err := secrets.ResolveOrInfer(webhookURLFile, "discord", inst.Name, "webhook_url", webhookURLKey, log)
		if err != nil {
			return nil, err
		}
		discordCfg = discord.Config{
			WebhookURL:   webhookURL,
			Username:     inst.Username,
			Template:     inst.Template,
			Mode:         inst.Mode,
			MessageStore: msgstore.NewMemoryStore(0, 0),
		}
	}

	handler, err := discord.New(discordCfg, log)
	if err != nil {
		return nil, err
	}

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
