package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/sentry"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// SentryFactory builds ActionHandlers from Sentry instance configurations.
type SentryFactory struct{}

// Build creates action handlers for a single Sentry instance.
func (f *SentryFactory) Build(inst config.SentryInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	tokenFile, tokenKey := "", ""
	if inst.Auth != nil {
		tokenFile = inst.Auth.TokenFile
		tokenKey = inst.Auth.TokenKey
	}
	token, err := secrets.ResolveOrInfer(tokenFile, "sentry", inst.Name, "token", tokenKey, log)
	if err != nil {
		return nil, err
	}

	handler := sentry.New(sentry.Config{
		Name:     inst.Name,
		BaseURL:  inst.BaseURL,
		Token:    token,
		Org:      inst.Org,
		Projects: inst.Projects,
		Log:      log,
	})

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
