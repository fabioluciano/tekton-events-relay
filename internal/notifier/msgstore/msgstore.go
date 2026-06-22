// Package msgstore persists a single message reference (e.g. a Slack message
// "ts" or a Discord message ID) keyed by pipeline RunID. Upsert-mode chat
// notifiers use it to edit the original message as a run progresses instead of
// posting a new message on every state change.
//
// It is intentionally minimal and shared: the Slack and Discord notifiers both
// store an opaque string reference per RunID, so a single abstraction covers
// both. Backends fail open at the call site — a missing reference simply means
// "post a new message".
package msgstore

import (
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

// Store persists an opaque per-RunID message reference. Implementations are
// safe for concurrent use.
type Store interface {
	// Load returns the stored message reference for runID, if present.
	Load(runID string) (string, bool)
	// Save records the message reference for runID, replacing any prior value.
	Save(runID, ref string)
}

const (
	defaultCapacity = 1000
	defaultTTL      = 30 * time.Minute
)

// MemoryStore is an in-memory, TTL-bounded Store backed by an expirable LRU.
// It is per-pod: references are lost on restart and not shared across replicas.
// The underlying expirable LRU is internally synchronized.
type MemoryStore struct {
	cache *expirable.LRU[string, string]
}

// NewMemoryStore creates a MemoryStore. A non-positive ttl or capacity falls
// back to defaults (1000 entries, 30m TTL).
func NewMemoryStore(ttl time.Duration, capacity int) *MemoryStore {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &MemoryStore{cache: expirable.NewLRU[string, string](capacity, nil, ttl)}
}

// Load returns the stored reference for runID, if present and not expired.
func (m *MemoryStore) Load(runID string) (string, bool) {
	return m.cache.Get(runID)
}

// Save records ref for runID.
func (m *MemoryStore) Save(runID, ref string) {
	m.cache.Add(runID, ref)
}
