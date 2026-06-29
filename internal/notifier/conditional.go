package notifier

import (
	"context"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// ConditionalHandler wraps an ActionHandler with CEL guard.
// If the CEL expression evaluates to false, the inner handler is skipped.
// If the expression evaluates to true, the inner handler is invoked.
// If the program is nil, the inner handler is always invoked (no guard).
type ConditionalHandler struct {
	inner   ActionHandler
	program *cel.Program
	log     *zap.Logger
}

// NewConditionalHandler creates a conditional wrapper.
// If program is nil, always delegates to inner handler (no guard).
func NewConditionalHandler(inner ActionHandler, program *cel.Program, log *zap.Logger) ActionHandler {
	return &ConditionalHandler{
		inner:   inner,
		program: program,
		log:     log,
	}
}

// Name returns inner handler name.
func (c *ConditionalHandler) Name() string {
	return c.inner.Name()
}

// Type returns inner handler type.
func (c *ConditionalHandler) Type() ActionType {
	return c.inner.Type()
}

// Close delegates to the inner handler's Close method.
func (c *ConditionalHandler) Close() error {
	return c.inner.Close()
}

// Handle evaluates CEL expression before delegating.
// - If program is nil: always delegate
// - If CEL eval returns true: delegate to inner handler
// - If CEL eval returns false: log.Debug and return nil (skip)
// - If CEL eval returns error: log.Error and return error (fail-closed)
func (c *ConditionalHandler) Handle(ctx context.Context, e domain.Event) error {
	// No guard: always delegate
	if c.program == nil {
		return c.inner.Handle(ctx, e)
	}

	// Evaluate CEL expression
	result, err := c.program.Eval(e)
	if err != nil {
		c.log.Error("CEL evaluation failed",
			zap.String("handler", c.inner.Name()),
			zap.Error(err))
		return err
	}

	// Guard returned false: skip handler
	if !result {
		c.log.Info("action skipped by when condition",
			zap.String("handler", c.inner.Name()),
			zap.String("action_type", string(c.inner.Type())),
			zap.String("event_resource", string(e.Resource)),
			zap.String("event_state", string(e.State)),
			zap.String("event_run_name", e.RunName))
		return nil
	}

	// Guard passed: delegate to inner handler
	return c.inner.Handle(ctx, e)
}
