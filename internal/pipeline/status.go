package pipeline

import (
	"sync"
	"time"
)

// HandlerStatus is a point-in-time view of a handler's recent activity,
// exposed by the /readyz endpoint for troubleshooting.
type HandlerStatus struct {
	LastEventAt time.Time `json:"last_event_at,omitzero"`
	LastError   string    `json:"last_error,omitempty"`
	LastErrorAt time.Time `json:"last_error_at,omitzero"`
	Succeeded   uint64    `json:"succeeded"`
	Failed      uint64    `json:"failed"`
}

// StatusTracker records per-handler dispatch outcomes.
type StatusTracker struct {
	mu sync.RWMutex
	m  map[string]*HandlerStatus
}

// NewStatusTracker creates an empty tracker.
func NewStatusTracker() *StatusTracker {
	return &StatusTracker{m: make(map[string]*HandlerStatus)}
}

// Observe records the outcome of one handler execution.
func (t *StatusTracker) Observe(handler string, err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.m[handler]
	if !ok {
		s = &HandlerStatus{}
		t.m[handler] = s
	}
	now := time.Now().UTC()
	s.LastEventAt = now
	if err != nil {
		s.Failed++
		s.LastError = err.Error()
		s.LastErrorAt = now
	} else {
		s.Succeeded++
	}
}

// Snapshot returns a copy of all handler statuses.
func (t *StatusTracker) Snapshot() map[string]HandlerStatus {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make(map[string]HandlerStatus, len(t.m))
	for k, v := range t.m {
		out[k] = *v
	}
	return out
}
