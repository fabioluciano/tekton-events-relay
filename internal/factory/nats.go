package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/nats"
)

// NATSFactory builds NATS notifier handlers from configuration.
type NATSFactory struct{}

// Build creates ActionHandler instances for a given NATS provider instance.
func (f *NATSFactory) Build(inst config.NATSInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	// Resolve credentials file from auth block if present.
	var credsFile string
	if inst.Auth != nil {
		credsFile = inst.Auth.CredentialsFile
	}

	var tlsEnabled bool
	if inst.Auth != nil {
		tlsEnabled = inst.Auth.TLSEnabled
	}

	handler, err := nats.New(nats.Config{
		Name:            inst.Name,
		Servers:         inst.Servers,
		Subject:         inst.Subject,
		CredentialsFile: credsFile,
		TLSEnabled:      tlsEnabled,
		Log:             log,
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
