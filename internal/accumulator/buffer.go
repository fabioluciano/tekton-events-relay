// Package accumulator provides a concurrent-safe buffer for aggregating
// TaskRun events by PipelineRun UID, enabling pipeline summary comments.
package accumulator

import (
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// RunState tracks accumulated TaskRuns for a single PipelineRun.
type RunState struct {
	UID        string                   // PipelineRun UID
	Tasks      map[string]*domain.Event // TaskRun name -> latest event
	LastUpdate time.Time
}

// Buffer is the interface for storing and retrieving accumulated run state.
type Buffer interface {
	Add(uid string, event *domain.Event)
	Get(uid string) (*RunState, bool)
	Flush(uid string) (*RunState, bool)
	Close()
}

// LRUBuffer implements Buffer using golang-lru/v2 expirable LRU for automatic TTL-based eviction.
// It replaces the manual map+goroutine pattern of RunBuffer.
type LRUBuffer struct {
	mu    sync.Mutex
	cache *expirable.LRU[string, *RunState]
}

// NewLRUBuffer creates a buffer backed by an expirable LRU cache.
// TTL controls per-entry expiry; maxSize caps total entries.
func NewLRUBuffer(ttl time.Duration, maxSize int) *LRUBuffer {
	if maxSize <= 0 {
		maxSize = 1000
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	cache := expirable.NewLRU[string, *RunState](maxSize, nil, ttl)
	return &LRUBuffer{cache: cache}
}

// Add inserts or updates the event for the given pipeline run UID.
func (b *LRUBuffer) Add(uid string, event *domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.cache.Get(uid)
	if !ok {
		state = &RunState{UID: uid, Tasks: make(map[string]*domain.Event)}
	}
	if event.Resource == domain.ResourceTaskRun {
		state.Tasks[event.RunName] = event
	}
	state.LastUpdate = time.Now()
	b.cache.Add(uid, state)
}

// Get returns the current RunState for the given UID without removing it.
func (b *LRUBuffer) Get(uid string) (*RunState, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.cache.Get(uid)
	if !ok {
		return nil, false
	}
	cp := &RunState{
		UID:        state.UID,
		Tasks:      make(map[string]*domain.Event, len(state.Tasks)),
		LastUpdate: state.LastUpdate,
	}
	for k, v := range state.Tasks {
		cp.Tasks[k] = v
	}
	return cp, true
}

// Flush removes and returns the RunState for the given UID.
func (b *LRUBuffer) Flush(uid string) (*RunState, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.cache.Get(uid)
	if !ok {
		return nil, false
	}
	b.cache.Remove(uid)
	return state, true
}

// Close purges all entries. The expirable LRU manages its own cleanup goroutine.
func (b *LRUBuffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cache.Purge()
}
