package accumulator

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/store"
)

const storeOpTimeout = 5 * time.Second

// StoreBuffer adapts a store.RunBuffer to the Buffer interface used by the
// accumulator Handler. Backend failures fail open: errors are logged and
// counted, and the operation degrades (Add drops, Flush returns not-found)
// instead of failing the event pipeline.
type StoreBuffer struct {
	buffer     store.RunBuffer
	backend    string
	collectors *metrics.Collectors
	log        *zap.Logger
}

// NewStoreBuffer wraps a store.RunBuffer for use by the accumulator.
func NewStoreBuffer(rb store.RunBuffer, backend string, collectors *metrics.Collectors, log *zap.Logger) *StoreBuffer {
	if log == nil {
		log = zap.NewNop()
	}
	return &StoreBuffer{buffer: rb, backend: backend, collectors: collectors, log: log}
}

// Add stores the event for its task; the run UID keys the accumulation.
func (b *StoreBuffer) Add(uid string, event *domain.Event) {
	if event.Resource != domain.ResourceTaskRun {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	defer cancel()
	if err := b.buffer.Add(ctx, uid, event.RunName, event); err != nil {
		b.observeError("add")
		b.log.Warn("accumulator store unavailable, task event not buffered",
			zap.String("uid", uid),
			zap.String("task", event.RunName),
			zap.String("backend", b.backend),
			zap.Error(err))
	}
}

// Get is unsupported on remote backends; the accumulator Handler only flushes.
func (b *StoreBuffer) Get(_ string) (*RunState, bool) {
	return nil, false
}

// Flush removes and returns accumulated state for the run.
func (b *StoreBuffer) Flush(uid string) (*RunState, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	defer cancel()
	tasks, found, err := b.buffer.Flush(ctx, uid)
	if err != nil {
		b.observeError("flush")
		b.log.Warn("accumulator store unavailable, summary skipped",
			zap.String("uid", uid),
			zap.String("backend", b.backend),
			zap.Error(err))
		return nil, false
	}
	if !found {
		return nil, false
	}
	return &RunState{UID: uid, Tasks: tasks, LastUpdate: time.Now().UTC()}, true
}

// Close is a no-op; the underlying store lifecycle belongs to its owner.
func (b *StoreBuffer) Close() {}

func (b *StoreBuffer) observeError(op string) {
	if b.collectors != nil {
		b.collectors.StoreErrors.WithLabelValues(b.backend, op).Inc()
	}
}
