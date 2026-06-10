package store

import (
	"context"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	defaultDedupeCapacity = 10000
	defaultBufferCapacity = 1000
	defaultBufferTTL      = 30 * time.Second
)

// memoryStore is the default per-pod backend. State is lost on restart and
// not shared between replicas.
type memoryStore struct {
	dedupe *memoryDedupe
	buffer *memoryRunBuffer
}

func newMemoryStore(cfg config.StoreConfig, opts Options) *memoryStore {
	dedupeCap := opts.DedupeCapacity
	if dedupeCap <= 0 {
		dedupeCap = defaultDedupeCapacity
	}
	bufferCap := opts.BufferCapacity
	if bufferCap <= 0 {
		bufferCap = defaultBufferCapacity
	}
	bufferTTL := cfg.TTL
	if bufferTTL <= 0 {
		bufferTTL = defaultBufferTTL
	}
	var onEvict func(string, struct{})
	if opts.Collectors != nil {
		evictions := opts.Collectors.DeduperEvictions
		onEvict = func(string, struct{}) { evictions.Inc() }
	}
	cache, _ := lru.NewWithEvict[string, struct{}](dedupeCap, onEvict)
	return &memoryStore{
		dedupe: &memoryDedupe{cache: cache},
		buffer: &memoryRunBuffer{
			cache: expirable.NewLRU[string, map[string]*domain.Event](bufferCap, nil, bufferTTL),
		},
	}
}

func (m *memoryStore) Dedupe() DedupeStore  { return m.dedupe }
func (m *memoryStore) RunBuffer() RunBuffer { return m.buffer }
func (m *memoryStore) Backend() string      { return BackendMemory }
func (m *memoryStore) Close() error         { return nil }
func (m *memoryStore) DedupeLen() int       { return m.dedupe.Len() }

// memoryDedupe wraps a size-bounded LRU; entries expire by eviction.
type memoryDedupe struct {
	cache *lru.Cache[string, struct{}]
}

func (d *memoryDedupe) FirstSeen(_ context.Context, id string) (bool, error) {
	if _, exists := d.cache.Get(id); exists {
		return false, nil
	}
	d.cache.Add(id, struct{}{})
	return true, nil
}

// Len reports the current cache size, used for the dedupe_cache_size gauge.
func (d *memoryDedupe) Len() int { return d.cache.Len() }

// memoryRunBuffer keeps per-run task maps in an expirable LRU. Events are
// deep-copied on write and on flush so callers never share mutable state.
type memoryRunBuffer struct {
	mu    sync.Mutex
	cache *expirable.LRU[string, map[string]*domain.Event]
}

func (b *memoryRunBuffer) Add(_ context.Context, uid, task string, ev *domain.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	tasks, ok := b.cache.Get(uid)
	if !ok {
		tasks = make(map[string]*domain.Event)
	}
	cp := *ev
	tasks[task] = &cp
	b.cache.Add(uid, tasks)
	return nil
}

func (b *memoryRunBuffer) Flush(_ context.Context, uid string) (map[string]*domain.Event, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	tasks, ok := b.cache.Get(uid)
	if !ok {
		return nil, false, nil
	}
	b.cache.Remove(uid)

	out := make(map[string]*domain.Event, len(tasks))
	for k, v := range tasks {
		cp := *v
		out[k] = &cp
	}
	return out, true, nil
}
