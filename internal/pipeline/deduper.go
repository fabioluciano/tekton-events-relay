package pipeline

import (
	"context"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/store"
)

// Deduper discards events with duplicate CloudEvent IDs using a pluggable
// dedupe store (in-memory LRU by default, valkey/olric for shared state).
type Deduper struct {
	BaseHandler
	store      store.DedupeStore
	backend    string
	collectors *metrics.Collectors
	log        *zap.Logger
}

// NewDeduperWithStore creates a Deduper backed by the given store.
// Store failures fail open: the event is treated as first-seen so a backend
// outage degrades to possible duplicates instead of dropped notifications.
func NewDeduperWithStore(s store.DedupeStore, backend string, collectors *metrics.Collectors, log *zap.Logger) *Deduper {
	if log == nil {
		log = zap.NewNop()
	}
	return &Deduper{store: s, backend: backend, collectors: collectors, log: log}
}

// Handle checks if the event has been seen before. If so, drops it.
// Otherwise, records the event ID and passes it to the next handler.
func (d *Deduper) Handle(ctx context.Context, env *event.Envelope) error {
	first, err := d.store.FirstSeen(ctx, env.CloudEventID)
	if err != nil {
		// Fail open: process the event rather than lose it.
		if d.collectors != nil {
			d.collectors.StoreErrors.WithLabelValues(d.backend, "dedupe").Inc()
		}
		d.log.Warn("dedupe store unavailable, processing event without deduplication",
			zap.String("ce_id", env.CloudEventID),
			zap.String("backend", d.backend),
			zap.Error(err))
		return d.Next(ctx, env)
	}

	if !first {
		if d.collectors != nil {
			d.collectors.DeduperHits.Inc()
		}
		return nil
	}

	if d.collectors != nil {
		if sized, ok := d.store.(interface{ Len() int }); ok {
			d.collectors.DedupeCacheSize.Set(float64(sized.Len()))
		}
	}
	return d.Next(ctx, env)
}
