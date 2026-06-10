package middleware

import (
	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// WrapWithFilter wraps a handler with a filter guard if cfg is non-nil.
func WrapWithFilter(handler notifier.ActionHandler, filterCfg *config.ActionFilterConfig) notifier.ActionHandler {
	if filterCfg == nil {
		return handler
	}
	return notifier.NewFilteredHandler(handler, filterCfg)
}
