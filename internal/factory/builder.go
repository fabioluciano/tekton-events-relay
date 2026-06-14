package factory

import (
	"errors"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
)

// ErrUnsupportedActionType is returned by factory Build functions when an action type
// has no registered handler. buildActionsWithMiddleware silently skips it.
var ErrUnsupportedActionType = errors.New("unsupported action type")

// BuildAndRegister is a generic helper that builds handlers from a list of
// instance configs using the given factory and registers them in the registry.
// This eliminates the repetitive loop: instances -> factory.Build() -> register.
func BuildAndRegister[C any](
	instances []C,
	f HandlerFactory[C],
	log *zap.Logger,
	reg *notifier.Registry,
) error {
	for _, inst := range instances {
		handlers, err := f.Build(inst, log)
		if err != nil {
			return err
		}
		for _, h := range handlers {
			reg.Register(h)
		}
	}
	return nil
}

// buildActionsWithMiddleware iterates actions, invokes buildFn for each enabled action,
// wraps each handler with CEL condition and filter middleware, and returns the slice.
func buildActionsWithMiddleware(
	actions []config.Action,
	log *zap.Logger,
	buildFn func(action config.Action) (notifier.ActionHandler, error),
) ([]notifier.ActionHandler, error) {
	handlers := make([]notifier.ActionHandler, 0, len(actions))
	for _, action := range actions {
		if !action.Enabled {
			continue
		}
		handler, err := buildFn(action)
		if errors.Is(err, ErrUnsupportedActionType) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if handler == nil {
			continue
		}
		wrapped, err := middleware.WrapWithCEL(handler, action.When, log)
		if err != nil {
			return nil, err
		}
		wrapped = middleware.WrapWithFilter(wrapped, action.Filter)
		if action.Type == notifier.ActionCommitStatus {
			wrapped = middleware.WrapWithContextPerTask(wrapped, action.ContextPerTask)
		}
		handlers = append(handlers, wrapped)
	}
	return handlers, nil
}
