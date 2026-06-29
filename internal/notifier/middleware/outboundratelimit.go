package middleware

import (
	"context"

	"golang.org/x/time/rate"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// WrapWithRateLimit wraps a handler with outbound rate limiting using the
// provided limiter. When limiter is nil the handler passes through unchanged.
func WrapWithRateLimit(handler notifier.ActionHandler, limiter *rate.Limiter) notifier.ActionHandler {
	if limiter == nil {
		return handler
	}
	return &rateLimitedHandler{inner: handler, limiter: limiter}
}

type rateLimitedHandler struct {
	inner   notifier.ActionHandler
	limiter *rate.Limiter
}

func (h *rateLimitedHandler) Name() string              { return h.inner.Name() }
func (h *rateLimitedHandler) Type() notifier.ActionType { return h.inner.Type() }

func (h *rateLimitedHandler) Handle(ctx context.Context, e domain.Event) error {
	if err := h.limiter.Wait(ctx); err != nil {
		return err
	}
	return h.inner.Handle(ctx, e)
}

func (h *rateLimitedHandler) Close() error { return h.inner.Close() }
