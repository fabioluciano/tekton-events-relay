package notifier

import (
	"context"
	"fmt"
)

// notifierDedupePrefix separates the notifier dedupe key space from the
// pipeline dedupe key space so entries do not collide.
const notifierDedupePrefix = "notif"

// DedupeStore is the minimal interface the notifier dedupe system needs
// from a state backend. Defined locally to avoid importing the store
// package (which depends on config, which depends on notifier).
type DedupeStore interface {
	// FirstSeen atomically records id and reports whether this is the first
	// time it is observed.
	FirstSeen(ctx context.Context, id string) (bool, error)
}

// NotifierDedupeStore records notifications by (handler_name, event_id)
// composite key and answers whether a pair has already been dispatched.
// It delegates to the shared DedupeStore backend but uses a distinct
// key prefix so pipeline dedupe keys and notifier dedupe keys never collide.
//
//nolint:revive // Cannot rename to DedupeStore (already exists in package)
type NotifierDedupeStore interface {
	// FirstSeen atomically records (handlerName, eventID) and reports whether
	// this is the first time this pair is observed.
	FirstSeen(ctx context.Context, handlerName, eventID string) (bool, error)
}

// notifierDedupeStore wraps a DedupeStore with a composite key scheme.
type notifierDedupeStore struct {
	inner DedupeStore
}

// NewNotifierDedupeStore creates a NotifierDedupeStore that writes through
// to the shared store backend. Pass the store's Dedupe() as inner.
func NewNotifierDedupeStore(inner DedupeStore) NotifierDedupeStore {
	return &notifierDedupeStore{inner: inner}
}

// compositeKey builds "notif:{handler}:{event}" so keys never collide with
// pipeline dedupe entries and are namespaced per handler.
func compositeKey(handlerName, eventID string) string {
	return fmt.Sprintf("%s:%s:%s", notifierDedupePrefix, handlerName, eventID)
}

// FirstSeen reports whether (handlerName, eventID) is new and records it.
func (s *notifierDedupeStore) FirstSeen(ctx context.Context, handlerName, eventID string) (bool, error) {
	return s.inner.FirstSeen(ctx, compositeKey(handlerName, eventID))
}

// compile-time check
var _ NotifierDedupeStore = (*notifierDedupeStore)(nil)
