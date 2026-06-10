// Package dlq implements a dead letter queue for events that failed with
// permanent (non-retryable) errors. Without it, a permanent failure (an
// expired token, a misconfigured repo) silently drops the event; the DLQ
// preserves it for inspection and replay via the HTTP API.
package dlq

import (
	"context"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// DeadEvent is a failed event preserved for inspection and replay.
type DeadEvent struct {
	ID         string          `json:"id"` // CloudEvent ID
	FailedAt   time.Time       `json:"failed_at"`
	Cause      string          `json:"cause"`
	RetryCount int             `json:"retry_count"` // times this event was replayed
	Envelope   *event.Envelope `json:"envelope"`
}

// Queue stores dead events.
type Queue interface {
	// Enqueue appends a failed event. The CloudEvent ID keys the entry;
	// re-enqueueing an existing ID replaces it and bumps RetryCount.
	Enqueue(ctx context.Context, env *event.Envelope, cause error) error
	// List returns up to limit dead events, oldest first.
	List(ctx context.Context, limit int) ([]DeadEvent, error)
	// Remove deletes the entry with the given CloudEvent ID.
	Remove(ctx context.Context, id string) error
	// Size reports the number of stored events.
	Size(ctx context.Context) (int, error)
	Close() error
}
