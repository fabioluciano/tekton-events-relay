// Package factory provides a generic factory pattern for building ActionHandlers
// from typed configuration structs, eliminating runtime type assertions.
package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// HandlerFactory builds ActionHandlers from a typed configuration struct.
// Each provider (GitHub, Slack, etc.) implements this with its own config type.
type HandlerFactory[C any] interface {
	// Build creates action handlers from the given instance configuration.
	Build(cfg C, log *zap.Logger) ([]notifier.ActionHandler, error)
}

// labelSet converts the action's labels block to the runtime LabelSet.
func labelSet(action config.Action) scm.LabelSet {
	if action.Labels == nil {
		return scm.LabelSet{}
	}
	add := make([]scm.Label, len(action.Labels.Add))
	for i, entry := range action.Labels.Add {
		add[i] = scm.Label{Name: entry.Name, Color: entry.Color}
	}
	remove := make([]scm.Label, len(action.Labels.Remove))
	for i, entry := range action.Labels.Remove {
		remove[i] = scm.Label{Name: entry.Name, Color: entry.Color}
	}
	return scm.LabelSet{Add: add, Remove: remove}
}
