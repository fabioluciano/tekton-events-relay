// Package store defines pluggable state backends for deduplication and
// event accumulation, so multi-replica deployments can share state.
//
// Backends: memory (default, per-pod), valkey (shared external server),
// olric (embedded distributed cache between relay pods).
package store

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
)

// Backend identifiers accepted in config.store.backend.
const (
	BackendMemory = "memory"
	BackendValkey = "valkey"
	BackendOlric  = "olric"
)

// DedupeStore records CloudEvent IDs and answers whether an ID was already seen.
type DedupeStore interface {
	// FirstSeen atomically records id and reports whether this is the first
	// time it is observed. Implementations expire entries after the
	// configured TTL (memory expires by LRU capacity instead).
	FirstSeen(ctx context.Context, id string) (bool, error)
}

// RunBuffer accumulates per-task events of a pipeline run, keyed by run UID.
type RunBuffer interface {
	// Add stores or replaces the latest event for a task of the given run.
	Add(ctx context.Context, uid, task string, ev *domain.Event) error
	// Flush atomically removes and returns all accumulated task events.
	// The bool reports whether the run existed.
	Flush(ctx context.Context, uid string) (map[string]*domain.Event, bool, error)
}

// Store bundles the backends sharing a single connection/lifecycle.
type Store interface {
	Dedupe() DedupeStore
	RunBuffer() RunBuffer
	Backend() string
	// Ping checks store connectivity. Memory always returns nil (no network);
	// Valkey sends PING; Olric checks cluster membership.
	Ping(ctx context.Context) error
	Close() error
}

// Options carries backend-independent construction parameters.
type Options struct {
	// DedupeCapacity bounds the memory dedupe cache (ignored by remote backends).
	DedupeCapacity int
	// BufferCapacity bounds the memory run buffer (ignored by remote backends).
	BufferCapacity int
	Log            *zap.Logger
	Collectors     *metrics.Collectors
}

// New builds the Store selected by cfg.Backend.
func New(cfg config.StoreConfig, opts Options) (Store, error) {
	if opts.Log == nil {
		opts.Log = zap.NewNop()
	}
	switch cfg.Backend {
	case "", BackendMemory:
		return newMemoryStore(cfg, opts), nil
	case BackendValkey:
		return newValkeyStore(cfg, opts)
	case BackendOlric:
		return newOlricStore(cfg, opts)
	default:
		return nil, fmt.Errorf("store: unknown backend %q (must be memory, valkey or olric)", cfg.Backend)
	}
}
