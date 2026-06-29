package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/telegram"
)

// TelegramFactory builds ActionHandlers from Telegram instance configurations.
type TelegramFactory struct{}

// Build creates action handlers for a single Telegram instance.
func (f *TelegramFactory) Build(inst config.TelegramInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	tokenFile, tokenKey := "", ""
	if inst.Auth != nil {
		tokenFile = inst.Auth.TokenFile
		tokenKey = inst.Auth.TokenKey
	}
	// Telegram bot token is embedded in the API URL, not a header. Re-read
	// the mounted secret per request so a rotated token is picked up without
	// a pod restart.
	token, err := resolveFileRefresher(tokenFile, tokenKey, "telegram", inst.Name, log)
	if err != nil {
		return nil, err
	}

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	handler, err := telegram.New(telegram.Config{
		Name:        inst.Name,
		Token:       token,
		ChatID:      inst.ChatID,
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
