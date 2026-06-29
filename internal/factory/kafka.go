package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/kafka"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
)

// KafkaFactory builds Kafka notifier handlers from configuration.
type KafkaFactory struct{}

// Build creates ActionHandler instances for a given Kafka provider instance.
func (f *KafkaFactory) Build(inst config.KafkaInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	handler, err := kafka.New(kafka.Config{
		Name:         inst.Name,
		Brokers:      inst.Brokers,
		Topic:        inst.Topic,
		RequiredAcks: inst.RequiredAcks,
		Log:          log,
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
