package factory

import (
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/batch"
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
			ThreadMode:   inst.ThreadMode,
			MentionRoles: inst.MentionRoles,
			MessageStore: msgstore.NewMemoryStore(0, 0),
		}
	} else {
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
			ThreadMode:   inst.ThreadMode,
			MentionRoles: inst.MentionRoles,
			MessageStore: msgstore.NewMemoryStore(0, 0),
		}
	}

	handler, err := discord.New(discordCfg, log)
	if err != nil {
		return nil, err
	}

	var h notifier.ActionHandler = handler
	if inst.Batch != nil && inst.Batch.Enabled {
		batchCfg := batch.Config{
			Enabled:       true,
			MaxSize:       inst.Batch.MaxSize,
			FlushInterval: inst.Batch.FlushInterval,
		}
		if batchCfg.FlushInterval == 0 {
			batchCfg.FlushInterval = 5 * time.Second
		}
		h = batch.New(handler, "discord", batchCfg, log, nil)
	}

	wrapped, err := middleware.WrapWithCEL(h, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
