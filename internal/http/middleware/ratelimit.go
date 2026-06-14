package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	rateLimiterTTL        = 5 * time.Minute
	rateLimiterCleanup    = 60 * time.Second
	maxKeyLength          = 256
	maxRateLimiterEntries = 10000
)

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter implements per-source rate limiting with TTL-based eviction.
type RateLimiter struct {
	entries  map[string]*entry
	mu       sync.RWMutex
	rps      float64
	burst    int
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
}

// NewRateLimiter creates a rate limiter with background cleanup.
func NewRateLimiter(requestsPerSecond float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		entries: make(map[string]*entry),
		rps:     requestsPerSecond,
		burst:   burst,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	if len(key) > maxKeyLength {
		key = key[:maxKeyLength]
	}
	if key == "" {
		key = "__fallback__"
	}

	rl.mu.RLock()
	e, exists := rl.entries[key]
	rl.mu.RUnlock()
	if exists {
		e.lastSeen = time.Now()
		return e.limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	// Double-check after acquiring write lock.
	if e, exists = rl.entries[key]; exists {
		e.lastSeen = time.Now()
		return e.limiter
	}
	if len(rl.entries) >= maxRateLimiterEntries {
		return rate.NewLimiter(rate.Limit(rl.rps), rl.burst)
	}
	e = &entry{limiter: rate.NewLimiter(rate.Limit(rl.rps), rl.burst)}
	e.lastSeen = time.Now()
	rl.entries[key] = e
	return e.limiter
}

func (rl *RateLimiter) cleanupLoop() {
	defer close(rl.doneCh)
	ticker := time.NewTicker(rateLimiterCleanup)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.evictStale()
		case <-rl.stopCh:
			return
		}
	}
}

// Stop terminates the background cleanup goroutine and waits for it to exit.
// It is safe to call multiple times.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() {
		close(rl.stopCh)
	})
	<-rl.doneCh
}

func (rl *RateLimiter) evictStale() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rateLimiterTTL)
	for key, e := range rl.entries {
		if e.lastSeen.Before(cutoff) {
			delete(rl.entries, key)
		}
	}
}

// Middleware returns an HTTP middleware that applies rate limiting.
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := strings.TrimSpace(r.Header.Get("Ce-Source"))
			if key == "" {
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					host = r.RemoteAddr
				}
				key = host
			}

			if !rl.getLimiter(key).Allow() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
