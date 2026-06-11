package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/grafana"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// GrafanaFactory builds ActionHandlers from Grafana instance configurations.
type GrafanaFactory struct{}

// Build creates action handlers for a single Grafana instance.
func (f *GrafanaFactory) Build(inst config.GrafanaInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	tokenFile, tokenKey := "", ""
	if inst.Auth != nil {
		tokenFile = inst.Auth.TokenFile
		tokenKey = inst.Auth.TokenKey
	}
	token, err := secrets.ResolveOrInfer(tokenFile, "grafana", inst.Name, "token", tokenKey, log)
	if err != nil {
		return nil, err
	}

	handler, err := grafana.New(grafana.Config{
		Name:     inst.Name,
		URL:      inst.URL,
		Token:    token,
		Tags:     inst.Tags,
		Template: inst.Template,
		Log:      log,
	})
	if err != nil {
		return nil, err
	}

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
