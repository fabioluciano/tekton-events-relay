package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/sentry"
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
	// Sentry's API uses an auth token (Bearer); it does not accept OAuth2
	// client_credentials. Re-read the mounted secret per request so a rotated
	// token is picked up without a pod restart.
	token, err := resolveFileRefresher(tokenFile, tokenKey, "sentry", inst.Name, log)
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
