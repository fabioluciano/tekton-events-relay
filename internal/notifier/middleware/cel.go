// Package middleware provides reusable handler wrappers for CEL guards and filters.
package middleware

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// WrapWithCEL wraps a handler with a CEL guard if whenExpr is non-empty.
func WrapWithCEL(handler notifier.ActionHandler, whenExpr string, log *zap.Logger) (notifier.ActionHandler, error) {
	if whenExpr == "" {
		return handler, nil
	}
	prog, err := cel.Compile(whenExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid CEL expression %q: %w", whenExpr, err)
	}
	return notifier.NewConditionalHandler(handler, prog, log), nil
}
