package pipeline

import (
	"context"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
)

// Deduper discards events with duplicate CloudEvent IDs using an LRU cache.
type Deduper struct {
	BaseHandler
	cache      *lru.Cache[string, struct{}]
	collectors *metrics.Collectors
}

// NewDeduper creates a Deduper with the specified LRU capacity.
func NewDeduper(capacity int, collectors *metrics.Collectors) *Deduper {
	if capacity <= 0 {
		capacity = 10000
	}
	c, _ := lru.New[string, struct{}](capacity)
	return &Deduper{cache: c, collectors: collectors}
}

// Handle checks if the event has been seen before. If so, drops it.
// Otherwise, records the event ID and passes it to the next handler.
func (d *Deduper) Handle(ctx context.Context, env *event.Envelope) error {
	if _, exists := d.cache.Get(env.CloudEventID); exists {
		if d.collectors != nil {
			d.collectors.DeduperHits.Inc()
		}
		return nil
	}
	d.cache.Add(env.CloudEventID, struct{}{})
	return d.Next(ctx, env)
}
