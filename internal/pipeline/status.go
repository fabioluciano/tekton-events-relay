package pipeline

import (
	"sync"
	"time"
)

const ringBufferSize = 100

// HandlerStatus is a point-in-time view of a handler's recent activity,
// exposed by the /readyz endpoint for troubleshooting.
type HandlerStatus struct {
	LastEventAt time.Time `json:"last_event_at,omitzero"`
	LastError   string    `json:"last_error,omitempty"`
	LastErrorAt time.Time `json:"last_error_at,omitzero"`
	Succeeded   uint64    `json:"succeeded"`
	Failed      uint64    `json:"failed"`
}

type handlerRing struct {
	entries [ringBufferSize]bool // true = success, false = failure
	index   int
	count   int
}

func (r *handlerRing) record(success bool) {
	r.entries[r.index] = success
	r.index = (r.index + 1) % ringBufferSize
	if r.count < ringBufferSize {
		r.count++
	}
}

func (r *handlerRing) errorRate() float64 {
	if r.count == 0 {
		return 0
	}
	failed := 0
	for i := 0; i < r.count; i++ {
		if !r.entries[i] {
			failed++
		}
	}
	return float64(failed) / float64(r.count)
}

func (r *handlerRing) hasRecentSuccess() bool {
	for i := 0; i < r.count; i++ {
		if r.entries[i] {
			return true
		}
	}
	return false
}

// StatusTracker records per-handler dispatch outcomes.
type StatusTracker struct {
	mu    sync.RWMutex
	m     map[string]*HandlerStatus
	rings map[string]*handlerRing
}

// NewStatusTracker creates an empty tracker.
func NewStatusTracker() *StatusTracker {
	return &StatusTracker{
		m:     make(map[string]*HandlerStatus),
		rings: make(map[string]*handlerRing),
	}
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

	r, ok := t.rings[handler]
	if !ok {
		r = &handlerRing{}
		t.rings[handler] = r
	}
	r.record(err == nil)
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

const degradedThreshold = 0.10

// DegradedHandlers returns a map of handler names whose error rate meets or exceeds the
// degraded threshold. Handlers with zero recent successes are excluded (those
// are unavailable, not degraded). Returns nil if the tracker is nil.
func (t *StatusTracker) DegradedHandlers() map[string]float64 {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make(map[string]float64)
	for name, r := range t.rings {
		if r.count == 0 {
			continue
		}
		rate := r.errorRate()
		if rate >= degradedThreshold && r.hasRecentSuccess() {
			out[name] = rate
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
