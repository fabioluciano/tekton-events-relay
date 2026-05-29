package pipeline

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"golang.org/x/sync/errgroup"
)

// HandlerSource provides access to registered action handlers.
type HandlerSource interface {
	All() []notifier.ActionHandler
}

// Dispatcher implements fan-out: calls ALL registered action handlers for
// each event. Handlers self-filter by provider match, required fields, and state.
type Dispatcher struct {
	BaseHandler
	registry HandlerSource
	log      *zap.Logger
}

// NewDispatcher creates a new Dispatcher with the given registry and logger.
func NewDispatcher(reg HandlerSource, log *zap.Logger) *Dispatcher {
	return &Dispatcher{registry: reg, log: log}
}

// Handle dispatches the event to all registered action handlers.
// Returns an error if any handler fails, but continues trying all handlers.
// Handlers return nil (skip) when provider doesn't match or required fields missing.
func (d *Dispatcher) Handle(ctx context.Context, env *event.Envelope) error {
	handlers := d.registry.All()
	if len(handlers) == 0 {
		d.log.Warn("no handlers registered, event dropped",
			zap.String("ce_id", env.CloudEventID),
			zap.String("run", env.Report.RunName),
		)
		return d.Next(ctx, env)
	}

	const maxConcurrency = 10

	var (
		mu   sync.Mutex
		errs []string
	)

	g := &errgroup.Group{}
	g.SetLimit(maxConcurrency)

	matchedCount := 0
	for _, h := range handlers {
		if env.Report.Provider != "" && env.Report.Provider != h.Name() {
			d.log.Debug("handler skipped: provider mismatch",
				zap.String("handler", h.Name()),
				zap.String("action", string(h.Type())),
				zap.String("event_provider", env.Report.Provider))
			continue
		}

		matchedCount++
		h := h // capture loop var
		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if err := h.Handle(ctx, env.Report); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s/%s: %v", h.Name(), h.Type(), err))
				mu.Unlock()
				d.log.Error("handler failed",
					zap.String("handler", h.Name()),
					zap.String("action", string(h.Type())),
					zap.String("runID", env.Report.RunName),
					zap.String("state", string(env.Report.State)),
					zap.Error(err),
				)
			} else {
				d.log.Debug("handler succeeded",
					zap.String("handler", h.Name()),
					zap.String("action", string(h.Type())),
					zap.String("runID", env.Report.RunName),
					zap.String("state", string(env.Report.State)),
				)
			}
			return nil
		})
	}

	_ = g.Wait()

	handledCount := matchedCount - len(errs)

	// Observability: warn if zero handlers actually processed the event
	if handledCount == 0 && len(handlers) > 0 {
		d.log.Warn("no handlers processed event",
			zap.String("event_id", env.CloudEventID),
			zap.String("provider", env.Report.Provider),
			zap.Int("total_handlers", len(handlers)))
	}

	if len(errs) > 0 {
		return fmt.Errorf("handler errors: %s", strings.Join(errs, "; "))
	}
	return d.Next(ctx, env)
}
