// Package pipeline implements Chain of Responsibility for processing
// CloudEvent events received from Tekton.
package pipeline

import (
	"context"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// Handler is a link in the chain.
type Handler interface {
	Handle(ctx context.Context, env *event.Envelope) error
	SetNext(h Handler)
}

// BaseHandler embeds the reference to the next link. Composition instead of inheritance.
type BaseHandler struct {
	next Handler
}

// SetNext sets the next handler in the chain.
func (b *BaseHandler) SetNext(h Handler) { b.next = h }

// Next calls the next handler if it exists; no-op if it's the last.
func (b *BaseHandler) Next(ctx context.Context, env *event.Envelope) error {
	if b.next == nil {
		return nil
	}
	return b.next.Handle(ctx, env)
}

// Build chains the handlers in the provided order and returns the first.
// Usage: chain := pipeline.Build(v, f, d, e, x)
func Build(handlers ...Handler) Handler {
	if len(handlers) == 0 {
		return nil
	}
	for i := 0; i < len(handlers)-1; i++ {
		handlers[i].SetNext(handlers[i+1])
	}
	return handlers[0]
}
