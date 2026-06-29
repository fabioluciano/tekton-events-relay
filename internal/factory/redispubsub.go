package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/redispubsub"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// RedisPubSubFactory builds Redis Pub/Sub notifier handlers from configuration.
type RedisPubSubFactory struct{}

// Build creates ActionHandler instances for a given Redis Pub/Sub provider instance.
func (f *RedisPubSubFactory) Build(inst config.RedisPubSubInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	var password string
	pwFile := inst.PasswordFile
	if pwFile == "" && inst.Auth != nil {
		pwFile = inst.Auth.PasswordFile
	}
	if pwFile != "" {
		var err error
		password, err = secrets.Resolve(pwFile, log)
		if err != nil {
			return nil, err
		}
	}

	handler, err := redispubsub.New(redispubsub.Config{
		Name:     inst.Name,
		Address:  inst.Address,
		Channel:  inst.Channel,
		Password: password,
		DB:       inst.DB,
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
