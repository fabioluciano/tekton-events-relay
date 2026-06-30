package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// sleepyHandler blocks until its context is canceled or sleep elapses.
type sleepyHandler struct {
	sleep time.Duration
}

func (h *sleepyHandler) Name() string              { return "sleepy" }
func (h *sleepyHandler) Provider() string          { return "sleepy" }
func (h *sleepyHandler) Type() notifier.ActionType { return notifier.ActionNotify }
func (h *sleepyHandler) Close() error              { return nil }

func (h *sleepyHandler) Handle(ctx context.Context, _ domain.Event) error {
	select {
	case <-time.After(h.sleep):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type sleepySource struct{ h notifier.ActionHandler }

func (s sleepySource) All() []notifier.ActionHandler          { return []notifier.ActionHandler{s.h} }
func (s sleepySource) Lookup(_ string) notifier.ActionHandler { return s.h }

func TestDispatcher_HandlerTimeoutAborts(t *testing.T) {
	collectors := metrics.NewCollectors(prometheus.NewRegistry())
	d := NewDispatcher(sleepySource{&sleepyHandler{sleep: 5 * time.Second}}, zap.NewNop(), collectors, 10).
		WithHandlerTimeout(50 * time.Millisecond)
	Build(d)

	start := time.Now()
	err := d.Handle(context.Background(), sample("timeout-evt"))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error from handler")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("dispatch took %v, want well under the handler sleep", elapsed)
	}
	if got := testutil.ToFloat64(collectors.HandlerTimeouts.WithLabelValues("sleepy/sleepy")); got != 1 {
		t.Errorf("handler_timeouts_total = %v, want 1", got)
	}
}

func TestDispatcher_NoTimeoutByDefault(t *testing.T) {
	collectors := metrics.NewCollectors(prometheus.NewRegistry())
	d := NewDispatcher(sleepySource{&sleepyHandler{sleep: 10 * time.Millisecond}}, zap.NewNop(), collectors, 10)
	Build(d)

	if err := d.Handle(context.Background(), sample("fast-evt")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := testutil.ToFloat64(collectors.HandlerTimeouts.WithLabelValues("sleepy/sleepy")); got != 0 {
		t.Errorf("handler_timeouts_total = %v, want 0", got)
	}
}
