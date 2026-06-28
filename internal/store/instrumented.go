package store

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// InstrumentedStore wraps a Store with latency and error metrics. Every
// operation that touches the backend is timed on StoreDuration and errors
// are counted on StoreOpErrors (both labelled by backend and operation).
//
// Usage:
//
//	st, err := store.New(...)
//	st = store.NewInstrumentedStore(st, collectors.StoreDuration, collectors.StoreOpErrors)
type InstrumentedStore struct {
	inner    Store
	backend  string
	duration *prometheus.HistogramVec // {backend, operation}
	errors   *prometheus.CounterVec   // {backend, operation}
}

// NewInstrumentedStore wraps inner and records metrics against the given
// collectors. It reads inner.Backend() once and reuses the value so the
// label is stable for the wrapper's lifetime.
func NewInstrumentedStore(inner Store, duration *prometheus.HistogramVec, errors *prometheus.CounterVec) *InstrumentedStore {
	return &InstrumentedStore{
		inner:    inner,
		backend:  inner.Backend(),
		duration: duration,
		errors:   errors,
	}
}

// Dedupe returns a DedupeStore instrumented with operation metrics.
func (s *InstrumentedStore) Dedupe() DedupeStore {
	return &instrumentedDedupeStore{
		inner:    s.inner.Dedupe(),
		backend:  s.backend,
		duration: s.duration,
		errors:   s.errors,
	}
}

// RunBuffer returns a RunBuffer instrumented with operation metrics.
func (s *InstrumentedStore) RunBuffer() RunBuffer {
	return &instrumentedRunBuffer{
		inner:    s.inner.RunBuffer(),
		backend:  s.backend,
		duration: s.duration,
		errors:   s.errors,
	}
}

// Backend returns the underlying store's backend identifier (no metric — O(1)
// getter that never errors).
func (s *InstrumentedStore) Backend() string { return s.backend }

// Ping checks the store backend health and records latency and errors.
func (s *InstrumentedStore) Ping(ctx context.Context) error {
	start := time.Now()
	err := s.inner.Ping(ctx)
	s.duration.WithLabelValues(s.backend, "ping").Observe(time.Since(start).Seconds())
	if err != nil {
		s.errors.WithLabelValues(s.backend, "ping").Inc()
	}
	return err
}

// Close shuts down the store backend and records latency and errors.
func (s *InstrumentedStore) Close() error {
	start := time.Now()
	err := s.inner.Close()
	s.duration.WithLabelValues(s.backend, "close").Observe(time.Since(start).Seconds())
	if err != nil {
		s.errors.WithLabelValues(s.backend, "close").Inc()
	}
	return err
}

// instrumentedDedupeStore wraps a DedupeStore with operation metrics.
type instrumentedDedupeStore struct {
	inner    DedupeStore
	backend  string
	duration *prometheus.HistogramVec
	errors   *prometheus.CounterVec
}

// FirstSeen atomically records id and reports whether this is the first
// time it is observed. Latency and errors are recorded on the shared
// StoreDuration / StoreOpErrors metrics.
func (d *instrumentedDedupeStore) FirstSeen(ctx context.Context, id string) (bool, error) {
	start := time.Now()
	first, err := d.inner.FirstSeen(ctx, id)
	d.duration.WithLabelValues(d.backend, "dedupe").Observe(time.Since(start).Seconds())
	if err != nil {
		d.errors.WithLabelValues(d.backend, "dedupe").Inc()
	}
	return first, err
}

// instrumentedRunBuffer wraps a RunBuffer with operation metrics.
type instrumentedRunBuffer struct {
	inner    RunBuffer
	backend  string
	duration *prometheus.HistogramVec
	errors   *prometheus.CounterVec
}

// Add stores or replaces the latest event for a task.
func (b *instrumentedRunBuffer) Add(ctx context.Context, uid, task string, ev *domain.Event) error {
	start := time.Now()
	err := b.inner.Add(ctx, uid, task, ev)
	b.duration.WithLabelValues(b.backend, "buffer_add").Observe(time.Since(start).Seconds())
	if err != nil {
		b.errors.WithLabelValues(b.backend, "buffer_add").Inc()
	}
	return err
}

// Flush atomically removes and returns all accumulated task events.
func (b *instrumentedRunBuffer) Flush(ctx context.Context, uid string) (map[string]*domain.Event, bool, error) {
	start := time.Now()
	tasks, found, err := b.inner.Flush(ctx, uid)
	b.duration.WithLabelValues(b.backend, "buffer_flush").Observe(time.Since(start).Seconds())
	if err != nil {
		b.errors.WithLabelValues(b.backend, "buffer_flush").Inc()
	}
	return tasks, found, err
}
