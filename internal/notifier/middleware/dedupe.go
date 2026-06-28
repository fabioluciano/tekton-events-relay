package middleware

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// WrapWithDedupe wraps a handler with a dedupe guard. When store is nil
// the handler passes through unchanged (no dedupe).
func WrapWithDedupe(handler notifier.ActionHandler, store notifier.NotifierDedupeStore, log *zap.Logger) notifier.ActionHandler {
	if store == nil {
		return handler
	}
	return notifier.NewDedupeHandler(handler, store, log)
}
