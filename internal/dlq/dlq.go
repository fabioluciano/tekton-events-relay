// Package dlq implements a dead letter queue for events that failed with
// permanent (non-retryable) errors. Without it, a permanent failure (an
// expired token, a misconfigured repo) silently drops the event; the DLQ
// preserves it for inspection and replay via the HTTP API.
package dlq

import (
	"context"
	"errors"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// ErrStopScan is returned by a Scan callback to stop iteration early without error.
var ErrStopScan = errors.New("stop scan")

// DeadEvent is a failed event preserved for inspection and replay.
type DeadEvent struct {
	ID         string          `json:"id"` // CloudEvent ID
	FailedAt   time.Time       `json:"failed_at"`
	Cause      string          `json:"cause"`
	RetryCount int             `json:"retry_count"` // times this event was replayed
	Envelope   *event.Envelope `json:"envelope"`
}

// ReplayFilter narrows replay to matching dead events.
// Zero-value fields are ignored (no constraint).
type ReplayFilter struct {
	Provider string    `json:"provider,omitempty"`
	State    string    `json:"state,omitempty"`
	After    time.Time `json:"after,omitempty"`
}

// Matches reports whether the dead event satisfies all non-zero filter fields.
func (f ReplayFilter) Matches(e DeadEvent) bool {
	if f.Provider != "" {
		if e.Envelope == nil || e.Envelope.Report.Provider != f.Provider {
			return false
		}
	}
	if f.State != "" {
		if e.Envelope == nil || string(e.Envelope.Report.State) != f.State {
			return false
		}
	}
	if !f.After.IsZero() && e.FailedAt.Before(f.After) {
		return false
	}
	return true
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
	// Scan iterates over all dead events without loading the full file into
	// memory. The callback is invoked once per entry; returning a non-nil
	// error from fn stops iteration and propagates the error.
	Scan(ctx context.Context, fn func(DeadEvent) error) error
	Close() error
}
