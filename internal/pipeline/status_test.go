package pipeline

import (
	"errors"
	"testing"
)

func TestDegradedState(t *testing.T) {
	tests := []struct {
		name         string
		successes    int
		failures     int
		wantDegraded bool
	}{
		{name: "no data", successes: 0, failures: 0, wantDegraded: false},
		{name: "all success", successes: 100, failures: 0, wantDegraded: false},
		{name: "5 percent failure", successes: 95, failures: 5, wantDegraded: false},
		{name: "10 percent failure", successes: 90, failures: 10, wantDegraded: true},
		{name: "15 failures in 100", successes: 85, failures: 15, wantDegraded: true},
		{name: "50 percent failure", successes: 50, failures: 50, wantDegraded: true},
		{name: "all failure", successes: 0, failures: 100, wantDegraded: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewStatusTracker()
			for i := 0; i < tt.successes; i++ {
				tracker.Observe("handler", nil)
			}
			for i := 0; i < tt.failures; i++ {
				tracker.Observe("handler", errors.New("fail"))
			}

			degraded := tracker.DegradedHandlers()
			if tt.wantDegraded {
				if degraded == nil {
					t.Fatalf("expected handler to be degraded, got nil")
				}
				if _, ok := degraded["handler"]; !ok {
					t.Fatalf("expected 'handler' in degraded map, got %v", degraded)
				}
			} else if degraded != nil {
				if _, ok := degraded["handler"]; ok {
					t.Fatalf("handler should not be degraded, got rate %v", degraded["handler"])
				}
			}
		})
	}
}

func TestDegradedState_RingBufferWindow(t *testing.T) {
	tracker := NewStatusTracker()

	for i := 0; i < 100; i++ {
		tracker.Observe("handler", errors.New("fail"))
	}

	degraded := tracker.DegradedHandlers()
	if degraded != nil {
		t.Fatalf("all-failure handler should not be degraded (unavailable), got %v", degraded)
	}

	for i := 0; i < 90; i++ {
		tracker.Observe("handler", nil)
	}

	degraded = tracker.DegradedHandlers()
	if degraded == nil {
		t.Fatal("expected handler to be degraded after successes mixed in")
	}
	if _, ok := degraded["handler"]; !ok {
		t.Fatalf("expected 'handler' in degraded map, got %v", degraded)
	}
}

func TestDegradedState_MultipleHandlers(t *testing.T) {
	tracker := NewStatusTracker()

	for i := 0; i < 100; i++ {
		tracker.Observe("healthy", nil)
	}

	for i := 0; i < 80; i++ {
		tracker.Observe("degraded", nil)
	}
	for i := 0; i < 20; i++ {
		tracker.Observe("degraded", errors.New("fail"))
	}

	for i := 0; i < 100; i++ {
		tracker.Observe("unavailable", errors.New("fail"))
	}

	degraded := tracker.DegradedHandlers()
	if degraded == nil {
		t.Fatal("expected degraded handlers")
	}
	if _, ok := degraded["healthy"]; ok {
		t.Error("healthy handler should not be degraded")
	}
	if _, ok := degraded["degraded"]; !ok {
		t.Error("degraded handler should be in degraded map")
	}
	if _, ok := degraded["unavailable"]; ok {
		t.Error("unavailable handler should not be in degraded map (no recent success)")
	}
}

func TestDegradedState_NilTracker(t *testing.T) {
	var tracker *StatusTracker
	if degraded := tracker.DegradedHandlers(); degraded != nil {
		t.Fatalf("nil tracker should return nil, got %v", degraded)
	}
}
