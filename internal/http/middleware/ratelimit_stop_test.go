package middleware

import (
	"testing"
	"time"
)

func TestRateLimiter_StopTerminatesCleanup(t *testing.T) {
	rl := NewRateLimiter(10, 10)

	done := make(chan struct{})
	go func() {
		rl.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2s")
	}
}

func TestRateLimiter_StopIsIdempotent(_ *testing.T) {
	rl := NewRateLimiter(10, 10)
	rl.Stop()
	rl.Stop() // must not panic or hang
}
