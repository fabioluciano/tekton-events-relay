package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/grafana"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
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
	// Grafana's API auth is a service-account token (Bearer); it does not accept
	// OAuth2 client_credentials. Re-read the mounted secret per request so a
	// rotated token is picked up without a pod restart.
	token, err := resolveFileRefresher(tokenFile, tokenKey, "grafana", inst.Name, log)
	if err != nil {
		return nil, err
	}

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	handler, err := grafana.New(grafana.Config{
		Name:         inst.Name,
		URL:          inst.URL,
		Token:        token,
		Tags:         inst.Tags,
		Template:     inst.Template,
		DashboardUID: inst.DashboardUID,
		PanelID:      inst.PanelID,
		Log:          log,
		HTTPClient:   httpClient,
		RetryPolicy:  retryPolicy,
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
