package factory

import (
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/batch"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/msgstore"
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

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	var channelExpr *cel.StringProgram
	if inst.ChannelExpr != "" {
		p, err := cel.CompileString(inst.ChannelExpr)
		if err != nil {
			return nil, fmt.Errorf("slack %s: invalid channel_expr: %w", inst.Name, err)
		}
		channelExpr = p
	}

	var slackCfg slack.Config
	if inst.Auth != nil && inst.Auth.BotToken != nil {
		refresher, err := resolveFileRefresher(inst.Auth.BotToken.TokenFile, inst.Auth.BotToken.TokenKey, "slack", inst.Name, log)
		if err != nil {
			return nil, err
		}
		slackCfg = slack.Config{
			Token:        refresher,
			ChannelID:    inst.Auth.BotToken.ChannelID,
			Channel:      inst.Channel,
			ChannelExpr:  channelExpr,
			Username:     inst.Username,
			IconEmoji:    inst.IconEmoji,
			Template:     inst.Template,
			Mode:         inst.Mode,
			ThreadMode:   inst.ThreadMode,
			ThreadTS:     inst.ThreadTS,
			MessageStore: msgstore.NewMemoryStore(0, 0),
		}
	} else {
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
			WebhookURL:  webhookURL,
			Channel:     inst.Channel,
			ChannelExpr: channelExpr,
			Username:    inst.Username,
			IconEmoji:   inst.IconEmoji,
			Template:    inst.Template,
			HTTPClient:  httpClient,
			RetryPolicy: retryPolicy,
		}
	}

	handler, err := slack.New(slackCfg, log)
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
		h = batch.New(handler, "slack", batchCfg, log, nil)
	}

	wrapped, err := middleware.WrapWithCEL(h, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
