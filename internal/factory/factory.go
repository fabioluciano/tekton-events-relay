// Package factory provides a generic factory pattern for building ActionHandlers
// from typed configuration structs, eliminating runtime type assertions.
package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// HandlerFactory builds ActionHandlers from a typed configuration struct.
// Each provider (GitHub, Slack, etc.) implements this with its own config type.
type HandlerFactory[C any] interface {
	// Build creates action handlers from the given instance configuration.
	Build(cfg C, log *zap.Logger) ([]notifier.ActionHandler, error)
}
