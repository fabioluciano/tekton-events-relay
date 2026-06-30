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

	apperrors "github.com/fabioluciano/tekton-events-relay/internal/errors"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// HandlerSource provides access to registered action handlers.
type HandlerSource interface {
	All() []notifier.ActionHandler
	Lookup(name string) notifier.ActionHandler
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
	fallbacks      map[string]string // handler name → fallback handler name
	fallbackOnly   map[string]bool   // handler names that are fallback-only (excluded from normal fan-out)
}

type handlerResult struct {
	handler notifier.ActionHandler
	err     error
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

// WithFallbacks configures fallback routing. fallbacks maps handler names to
// their fallback handler names. fallbackOnly lists handler names that should
// be excluded from normal fan-out (they are only invoked as fallbacks).
func (d *Dispatcher) WithFallbacks(fallbacks map[string]string, fallbackOnly map[string]bool) *Dispatcher {
	d.fallbacks = fallbacks
	d.fallbackOnly = fallbackOnly
	return d
}

// handlerKey returns a unique label for a handler combining provider and instance name.
// Format: "provider/name" (e.g., "gitlab/default", "github/prod").
func handlerKey(h notifier.ActionHandler) string {
	return h.Provider() + "/" + h.Name()
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
		if d.isFallbackOnly(h.Name()) {
			continue
		}
		if env.Report.Provider != "" && env.Report.Provider != h.Provider() && h.Type() != notifier.ActionNotify {
			continue
		}
		matched = append(matched, h)
	}

	if d.collectors != nil {
		d.collectors.EventsByStateTotal.WithLabelValues(string(env.Report.State)).Inc()
		d.collectors.EventsByProviderTotal.WithLabelValues(env.Report.Provider).Inc()
		d.collectors.EventsByResourceTotal.WithLabelValues(string(env.Report.Resource)).Inc()
	}

	var (
		mu      sync.Mutex
		errs    = make([]error, 0, len(matched))
		results = make([]handlerResult, 0, len(matched))
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
					attribute.String("handler.name", handlerKey(h)),
					attribute.String("handler.type", string(h.Type())),
				),
			)

			if d.handlerTimeout > 0 {
				var cancel context.CancelFunc
				hCtx, cancel = context.WithTimeout(hCtx, d.handlerTimeout)
				defer cancel()
			}

			hCtx = context.WithValue(hCtx, notifier.CloudEventIDKey, env.CloudEventID)

			start := time.Now()
			err := h.Handle(hCtx, env.Report)
			duration := time.Since(start)

			if err != nil && errors.Is(err, context.DeadlineExceeded) && d.collectors != nil {
				d.collectors.HandlerTimeouts.WithLabelValues(handlerKey(h)).Inc()
			}
			d.status.Observe(handlerKey(h), err)
			if d.collectors != nil {
				d.collectors.NotifierLatency.WithLabelValues(handlerKey(h), string(h.Type())).Observe(duration.Seconds())
			}

			if err != nil {
				hSpan.RecordError(err)
			}
			hSpan.End()

			status := "success"
			if err != nil {
				status = "error"
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s/%s: %w", handlerKey(h), h.Type(), err))
				results = append(results, handlerResult{handler: h, err: err})
				mu.Unlock()
				d.log.Error("handler failed",
					zap.String("handler", handlerKey(h)),
					zap.String("action", string(h.Type())),
					zap.String("runID", env.Report.RunName),
					zap.String("state", string(env.Report.State)),
					zap.Error(err),
				)
			} else {
				mu.Lock()
				results = append(results, handlerResult{handler: h, err: nil})
				mu.Unlock()
				fields := []zap.Field{
					zap.String("handler", handlerKey(h)),
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
				d.collectors.HandlerDuration.WithLabelValues(handlerKey(h)).Observe(duration.Seconds())
				d.collectors.EventsProcessed.WithLabelValues(handlerKey(h), status).Inc()
			}
			return nil
		})
	}

	waitErr := g.Wait()

	handledCount := len(matched) - len(errs)

	if handledCount == 0 && len(handlers) > 0 {
		d.log.Warn("no handlers processed event",
			zap.String("event_id", env.CloudEventID),
			zap.String("provider", env.Report.Provider),
			zap.Int("matched_handlers", len(matched)),
			zap.Int("total_handlers", len(handlers)))
	}

	if d.fallbacks != nil {
		fallbackErrs := d.executeFallbacks(ctx, env, results)
		errs = append(errs, fallbackErrs...)
	}

	allErrs := errs
	if waitErr != nil {
		allErrs = append(allErrs, waitErr)
	}
	if len(allErrs) > 0 {
		return errors.Join(allErrs...)
	}
	return d.Next(ctx, env)
}

func (d *Dispatcher) isFallbackOnly(name string) bool {
	if d.fallbackOnly == nil {
		return false
	}
	return d.fallbackOnly[name]
}

func (d *Dispatcher) executeFallbacks(ctx context.Context, env *event.Envelope, results []handlerResult) []error {
	var fallbackErrs []error
	for _, r := range results {
		if r.err == nil {
			continue
		}
		if apperrors.IsRetryable(r.err) {
			continue
		}
		fallbackName, ok := d.fallbacks[r.handler.Name()]
		if !ok || fallbackName == "" {
			continue
		}
		fb := d.registry.Lookup(fallbackName)
		if fb == nil {
			d.log.Warn("fallback handler not found",
				zap.String("primary", r.handler.Name()),
				zap.String("fallback", fallbackName),
			)
			continue
		}

		d.log.Info("invoking fallback handler",
			zap.String("primary", r.handler.Name()),
			zap.String("fallback", fallbackName),
			zap.String("runID", env.Report.RunName),
		)

		fbCtx := ctx
		if d.handlerTimeout > 0 {
			var cancel context.CancelFunc
			fbCtx, cancel = context.WithTimeout(ctx, d.handlerTimeout)
			defer cancel()
		}
		fbCtx = context.WithValue(fbCtx, notifier.CloudEventIDKey, env.CloudEventID)

		fbErr := fb.Handle(fbCtx, env.Report)
		if fbErr != nil {
			d.log.Error("fallback handler failed",
				zap.String("primary", r.handler.Name()),
				zap.String("fallback", fallbackName),
				zap.Error(fbErr),
			)
			fallbackErrs = append(fallbackErrs, fmt.Errorf("fallback %s for %s: %w", fallbackName, r.handler.Name(), errors.Join(r.err, fbErr)))
		} else {
			d.log.Info("fallback handler succeeded",
				zap.String("primary", r.handler.Name()),
				zap.String("fallback", fallbackName),
			)
		}
	}
	return fallbackErrs
}
