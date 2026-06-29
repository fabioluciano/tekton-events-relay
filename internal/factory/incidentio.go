package factory //nolint:dupl // Factory structs are structurally similar by design

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/incidentio"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
)

// IncidentIOFactory builds ActionHandlers from Incident.io instance configurations.
type IncidentIOFactory struct{}

// Build creates action handlers for a single Incident.io instance.
func (f *IncidentIOFactory) Build(inst config.IncidentIOInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	apiKeyFile, apiKeyKey := "", ""
	if inst.Auth != nil {
		apiKeyFile = inst.Auth.APIKeyFile
		apiKeyKey = inst.Auth.APIKeyKey
	}
	// Incident.io's API uses a Bearer API key; re-read the mounted secret per
	// request so a rotated key is picked up without a pod restart.
	apiKey, err := resolveFileRefresher(apiKeyFile, apiKeyKey, "incidentio", inst.Name, log)
	if err != nil {
		return nil, err
	}

	httpClient, retryPolicy := buildNotifierClient(inst.RetryOverride)

	handler := incidentio.New(incidentio.Config{
		Name:           inst.Name,
		APIKey:         apiKey,
		SeverityID:     inst.SeverityID,
		IncidentTypeID: inst.IncidentTypeID,
		Visibility:     inst.Visibility,
		HTTPClient:     httpClient,
		RetryPolicy:    retryPolicy,
	}, log)

	wrapped, err := middleware.WrapWithCEL(handler, inst.When, log)
	if err != nil {
		return nil, err
	}
	return []notifier.ActionHandler{wrapped}, nil
}
