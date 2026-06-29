package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/rabbitmq"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// RabbitMQFactory builds RabbitMQ notifier handlers from configuration.
type RabbitMQFactory struct{}

// Build creates ActionHandler instances for a given RabbitMQ provider instance.
func (f *RabbitMQFactory) Build(inst config.RabbitMQInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	url, err := secrets.Resolve(inst.URLFile, log)
	if err != nil {
		return nil, err
	}

	handler, err := rabbitmq.New(rabbitmq.Config{
		Name:       inst.Name,
		URL:        url,
		Exchange:   inst.Exchange,
		RoutingKey: inst.RoutingKey,
		Log:        log,
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
