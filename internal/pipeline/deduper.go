package pipeline

import (
	"container/list"
	"context"
	"sync"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// Deduper discards events whose Ce-Id has already been seen. Necessary because
// tekton-events-controller can resend after restart (its internal cache
// is in-memory and resets on reload). Also protects against reentrancy and
// retries at higher levels (Knative Eventing, mesh, etc).
//
// Implementation: thread-safe LRU based on container/list + map. No deps.
type Deduper struct {
	BaseHandler
	mu       sync.Mutex
	capacity int
	index    map[string]*list.Element
	order    *list.List
}

// NewDeduper creates a new Deduper with the given capacity.
// If capacity is <= 0, defaults to 10000.
func NewDeduper(capacity int) *Deduper {
	if capacity <= 0 {
		capacity = 10000
	}
	return &Deduper{
		capacity: capacity,
		index:    make(map[string]*list.Element, capacity),
		order:    list.New(),
	}
}

func (d *Deduper) seen(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if elem, ok := d.index[id]; ok {
		d.order.MoveToFront(elem)
		return true
	}
	elem := d.order.PushFront(id)
	d.index[id] = elem
	if d.order.Len() > d.capacity {
		oldest := d.order.Back()
		if oldest != nil {
			d.order.Remove(oldest)
			delete(d.index, oldest.Value.(string))
		}
	}
	return false
}

// Handle checks if the event has been seen before. If so, drops it.
// Otherwise, records the event ID and passes it to the next handler.
func (d *Deduper) Handle(ctx context.Context, env *event.Envelope) error {
	if d.seen(env.CloudEventID) {
		return nil
	}
	return d.Next(ctx, env)
}
