package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"golang.org/x/sync/errgroup"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// HandlerSource provides access to registered action handlers.
type HandlerSource interface {
	All() []notifier.ActionHandler
}

// Dispatcher fans out events to all registered action handlers concurrently.
type Dispatcher struct {
	BaseHandler
	registry       HandlerSource
	log            *zap.Logger
	collectors     *metrics.Collectors
	maxConcurrency int
	handlerTimeout time.Duration
	status         *StatusTracker
}

// NewDispatcher creates a new Dispatcher with the given registry and logger.
func NewDispatcher(reg HandlerSource, log *zap.Logger, collectors *metrics.Collectors, maxConcurrency int) *Dispatcher {
	return &Dispatcher{
		registry:       reg,
		log:            log,
		collectors:     collectors,
		maxConcurrency: maxConcurrency,
	}
}

// WithHandlerTimeout sets a per-handler execution deadline so one slow
// provider cannot stall the whole dispatch. Zero disables the deadline.
func (d *Dispatcher) WithHandlerTimeout(timeout time.Duration) *Dispatcher {
	d.handlerTimeout = timeout
	return d
}

// WithStatusTracker records per-handler outcomes for the /readyz endpoint.
func (d *Dispatcher) WithStatusTracker(t *StatusTracker) *Dispatcher {
	d.status = t
	return d
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

	matched := make([]notifier.ActionHandler, 0, len(handlers))
	for _, h := range handlers {
		if env.Report.Provider != "" && env.Report.Provider != h.Name() && h.Type() != notifier.ActionNotify {
			continue
		}
		matched = append(matched, h)
	}

	var (
		mu   sync.Mutex
		errs = make([]error, 0, len(matched))
	)

	g := &errgroup.Group{}
	g.SetLimit(d.maxConcurrency)

	for _, h := range matched {
		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			tracer := otel.Tracer("tekton-events-relay")
			hCtx, hSpan := tracer.Start(ctx, "handler.execute",
				trace.WithAttributes(
					attribute.String("handler.name", h.Name()),
					attribute.String("handler.type", string(h.Type())),
				),
			)

			if d.handlerTimeout > 0 {
				var cancel context.CancelFunc
				hCtx, cancel = context.WithTimeout(hCtx, d.handlerTimeout)
				defer cancel()
			}

			start := time.Now()
			err := h.Handle(hCtx, env.Report)
			duration := time.Since(start)

			if err != nil && errors.Is(err, context.DeadlineExceeded) && d.collectors != nil {
				d.collectors.HandlerTimeouts.WithLabelValues(h.Name()).Inc()
			}
			d.status.Observe(h.Name(), err)
			if d.collectors != nil {
				d.collectors.NotifierLatency.WithLabelValues(h.Name(), string(h.Type())).Observe(duration.Seconds())
			}

			if err != nil {
				hSpan.RecordError(err)
			}
			hSpan.End()

			status := "success"
			if err != nil {
				status = "error"
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s/%s: %w", h.Name(), h.Type(), err))
				mu.Unlock()
				d.log.Error("handler failed",
					zap.String("handler", h.Name()),
					zap.String("action", string(h.Type())),
					zap.String("runID", env.Report.RunName),
					zap.String("state", string(env.Report.State)),
					zap.Error(err),
				)
			} else {
				// Build target description for better observability
				fields := []zap.Field{
					zap.String("handler", h.Name()),
					zap.String("action", string(h.Type())),
					zap.String("runID", env.Report.RunName),
					zap.String("state", string(env.Report.State)),
				}
				if env.Report.Repo.Owner != "" && env.Report.Repo.Name != "" {
					fields = append(fields, zap.String("repo", env.Report.Repo.Owner+"/"+env.Report.Repo.Name))
				}
				if env.Report.PRNumber != nil {
					fields = append(fields, zap.Int("pr", *env.Report.PRNumber))
				}
				if env.Report.IssueNumber != nil {
					fields = append(fields, zap.Int("issue", *env.Report.IssueNumber))
				}
				if env.Report.CommitSHA != "" {
					commitShort := env.Report.CommitSHA
					if len(commitShort) > 8 {
						commitShort = commitShort[:8]
					}
					fields = append(fields, zap.String("commit", commitShort))
				}
				d.log.Info("action_success", fields...)
			}
			if d.collectors != nil {
				d.collectors.HandlerDuration.WithLabelValues(h.Name()).Observe(duration.Seconds())
				d.collectors.EventsProcessed.WithLabelValues(h.Name(), status).Inc()
			}
			return nil
		})
	}

	_ = g.Wait()

	handledCount := len(matched) - len(errs)

	// Observability: warn if zero handlers actually processed the event
	if handledCount == 0 && len(handlers) > 0 {
		d.log.Warn("no handlers processed event",
			zap.String("event_id", env.CloudEventID),
			zap.String("provider", env.Report.Provider),
			zap.Int("matched_handlers", len(matched)),
			zap.Int("total_handlers", len(handlers)))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return d.Next(ctx, env)
}
