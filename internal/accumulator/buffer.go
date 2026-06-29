// Package accumulator provides a concurrent-safe buffer for aggregating
// TaskRun events by PipelineRun UID, enabling pipeline summary comments.
package accumulator

import (
	"context"
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

// GroupState tracks which PipelineRuns belong to an accumulator group
// and which have reached terminal state.
type GroupState struct {
	GroupID string          // group identifier
	Members map[string]bool // uid → isTerminal
}

// Buffer is the interface for storing and retrieving accumulated run state.
type Buffer interface {
	Add(ctx context.Context, uid string, event *domain.Event)
	Get(uid string) (*RunState, bool)
	Flush(ctx context.Context, uid string) (*RunState, bool)
	Close() error

	AddWithGroup(ctx context.Context, groupID, uid string, event *domain.Event)
	IsGroupComplete(groupID string) bool
	FlushGroup(ctx context.Context, groupID string) (*RunState, bool)
}

// LRUBuffer implements Buffer using golang-lru/v2 expirable LRU for automatic TTL-based eviction.
// It replaces the manual map+goroutine pattern of RunBuffer.
type LRUBuffer struct {
	mu     sync.Mutex
	cache  *expirable.LRU[string, *RunState]
	groups map[string]*GroupState // groupID → GroupState
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
	return &LRUBuffer{cache: cache, groups: make(map[string]*GroupState)}
}

// Add inserts or updates the event for the given pipeline run UID.
// The context is accepted for interface conformance; the in-memory LRU
// does not perform I/O so the context is unused.
func (b *LRUBuffer) Add(_ context.Context, uid string, event *domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.cache.Get(uid)
	if !ok {
		state = &RunState{UID: uid, Tasks: make(map[string]*domain.Event)}
	}
	if event.Resource == domain.ResourceTaskRun {
		state.Tasks[event.RunName] = event
	}
	state.LastUpdate = time.Now().UTC()
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
func (b *LRUBuffer) Flush(_ context.Context, uid string) (*RunState, bool) {
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
// Returns any error encountered during cleanup.
func (b *LRUBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cache.Purge()
	b.groups = make(map[string]*GroupState)
	return nil
}

// compositeKey builds the LRU cache key from groupID and uid.
// When groupID is empty, returns just uid (backward compatible).
func compositeKey(groupID, uid string) string {
	if groupID == "" {
		return uid
	}
	return groupID + ":" + uid
}

// AddWithGroup inserts or updates the event using a composite key (groupID, uid).
// Tracks the UID in the group's member set. When the event is a PipelineRun
// in a terminal state, marks the member as terminal.
func (b *LRUBuffer) AddWithGroup(_ context.Context, groupID, uid string, event *domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := compositeKey(groupID, uid)

	state, ok := b.cache.Get(key)
	if !ok {
		state = &RunState{UID: uid, Tasks: make(map[string]*domain.Event)}
	}
	if event.Resource == domain.ResourceTaskRun {
		state.Tasks[event.RunName] = event
	}
	state.LastUpdate = time.Now().UTC()
	b.cache.Add(key, state)

	gs, exists := b.groups[groupID]
	if !exists {
		gs = &GroupState{GroupID: groupID, Members: make(map[string]bool)}
		b.groups[groupID] = gs
	}
	if _, member := gs.Members[uid]; !member {
		gs.Members[uid] = false
	}
	if event.Resource == domain.ResourcePipelineRun && isTerminalState(event.State) {
		gs.Members[uid] = true
	}
}

// IsGroupComplete returns true when every member of the group has reached
// a terminal state. Returns false if the group does not exist.
func (b *LRUBuffer) IsGroupComplete(groupID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	gs, ok := b.groups[groupID]
	if !ok || len(gs.Members) == 0 {
		return false
	}
	for _, terminal := range gs.Members {
		if !terminal {
			return false
		}
	}
	return true
}

// FlushGroup removes all RunState entries for the group, merges their Tasks
// into a single RunState, and removes the GroupState. Returns the merged
// RunState or false if the group does not exist or has no tasks.
func (b *LRUBuffer) FlushGroup(_ context.Context, groupID string) (*RunState, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	gs, ok := b.groups[groupID]
	if !ok {
		return nil, false
	}

	merged := &RunState{
		UID:   groupID,
		Tasks: make(map[string]*domain.Event),
	}

	for uid := range gs.Members {
		key := compositeKey(groupID, uid)
		state, found := b.cache.Get(key)
		if !found {
			continue
		}
		for name, evt := range state.Tasks {
			_, exists := merged.Tasks[name]
			if !exists || state.LastUpdate.After(merged.LastUpdate) {
				merged.Tasks[name] = evt
			}
		}
		if state.LastUpdate.After(merged.LastUpdate) {
			merged.LastUpdate = state.LastUpdate
		}
		b.cache.Remove(key)
	}

	delete(b.groups, groupID)

	if len(merged.Tasks) == 0 {
		return nil, false
	}
	return merged, true
}
