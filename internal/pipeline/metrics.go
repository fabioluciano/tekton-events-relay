package pipeline

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// MetricsHandler wraps a Handler with Prometheus instrumentation.
type MetricsHandler struct {
	BaseHandler
	inner    Handler
	duration prometheus.Observer
	counter  *prometheus.CounterVec
	step     string
}

// WithMetrics wraps a handler with Prometheus timing and counter instrumentation.
func WithMetrics(h Handler, duration prometheus.Observer, counter *prometheus.CounterVec, step string) Handler {
	return &MetricsHandler{
		inner:    h,
		duration: duration,
		counter:  counter,
		step:     step,
	}
}

// Handle instruments the inner handler with timing and counter metrics.
func (m *MetricsHandler) Handle(ctx context.Context, env *event.Envelope) error {
	start := time.Now()
	err := m.inner.Handle(ctx, env)
	elapsed := time.Since(start)

	m.duration.Observe(elapsed.Seconds())

	if err != nil {
		m.counter.WithLabelValues(m.step, "error").Inc()
		return err
	}

	m.counter.WithLabelValues(m.step, "success").Inc()
	return nil
}

// SetNext sets the next handler on the inner handler.
func (m *MetricsHandler) SetNext(next Handler) {
	m.inner.SetNext(next)
}
