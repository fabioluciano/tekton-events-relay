package store

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// newTestInstrumentedStore builds an InstrumentedStore wrapping a memory store
// with fresh Prometheus metric vecs for testing.
func newTestInstrumentedStore(t *testing.T, opts Options) *InstrumentedStore {
	t.Helper()
	duration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "test_store_duration", Buckets: []float64{0.001, 0.01, 0.1}},
		[]string{"backend", "operation"},
	)
	errCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "test_store_errors"},
		[]string{"backend", "operation"},
	)
	inner := newMemoryStore(config.StoreConfig{TTL: time.Minute}, opts)
	return NewInstrumentedStore(inner, duration, errCounter)
}

func TestInstrumentedStore_Backend(t *testing.T) {
	is := newTestInstrumentedStore(t, Options{})
	if got := is.Backend(); got != BackendMemory {
		t.Errorf("Backend() = %q, want %q", got, BackendMemory)
	}
}

func TestInstrumentedStore_Ping(t *testing.T) {
	is := newTestInstrumentedStore(t, Options{})
	if err := is.Ping(context.Background()); err != nil {
		t.Errorf("Ping() unexpected error: %v", err)
	}
}

func TestInstrumentedStore_Close(t *testing.T) {
	is := newTestInstrumentedStore(t, Options{})
	if err := is.Close(); err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
	if err := is.Close(); err != nil {
		t.Errorf("second Close() unexpected error: %v", err)
	}
}

func TestInstrumentedStore_Dedupe_FirstSeen(t *testing.T) {
	is := newTestInstrumentedStore(t, Options{DedupeCapacity: 10})
	d := is.Dedupe()
	ctx := context.Background()

	first, err := d.FirstSeen(ctx, "evt-1")
	if err != nil {
		t.Fatalf("FirstSeen unexpected error: %v", err)
	}
	if !first {
		t.Error("FirstSeen should be true for new ID")
	}

	again, err := d.FirstSeen(ctx, "evt-1")
	if err != nil {
		t.Fatalf("FirstSeen repeat unexpected error: %v", err)
	}
	if again {
		t.Error("FirstSeen should be false for duplicate ID")
	}

	other, err := d.FirstSeen(ctx, "evt-2")
	if err != nil {
		t.Fatalf("FirstSeen other ID unexpected error: %v", err)
	}
	if !other {
		t.Error("FirstSeen should be true for different ID")
	}
}

func TestInstrumentedStore_RunBuffer_AddFlush(t *testing.T) {
	is := newTestInstrumentedStore(t, Options{BufferCapacity: 10})
	rb := is.RunBuffer()
	ctx := context.Background()

	if err := rb.Add(ctx, "uid-1", "build", sampleEvent("build", domain.StateRunning)); err != nil {
		t.Fatalf("Add unexpected error: %v", err)
	}
	if err := rb.Add(ctx, "uid-1", "test", sampleEvent("test", domain.StateSuccess)); err != nil {
		t.Fatalf("Add test unexpected error: %v", err)
	}

	tasks, found, err := rb.Flush(ctx, "uid-1")
	if err != nil {
		t.Fatalf("Flush unexpected error: %v", err)
	}
	if !found {
		t.Fatal("Flush should find the entry")
	}
	if len(tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(tasks))
	}
	if tasks["build"].State != domain.StateRunning {
		t.Errorf("build state = %q, want %q", tasks["build"].State, domain.StateRunning)
	}
	if tasks["test"].State != domain.StateSuccess {
		t.Errorf("test state = %q, want %q", tasks["test"].State, domain.StateSuccess)
	}

	if _, found, _ := rb.Flush(ctx, "uid-1"); found {
		t.Error("second Flush should report not found")
	}
}

func TestInstrumentedStore_RunBuffer_FlushNonexistent(t *testing.T) {
	is := newTestInstrumentedStore(t, Options{})
	ctx := context.Background()

	_, found, err := is.RunBuffer().Flush(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Flush nonexistent unexpected error: %v", err)
	}
	if found {
		t.Error("Flush on nonexistent key should be false")
	}
}

func TestInstrumentedStore_Dedupe_MultipleCalls(t *testing.T) {
	is := newTestInstrumentedStore(t, Options{DedupeCapacity: 100})
	d := is.Dedupe()
	ctx := context.Background()

	tests := []struct {
		id   string
		want bool
	}{
		{"a", true},
		{"b", true},
		{"a", false},
		{"c", true},
		{"b", false},
		{"d", true},
	}

	for _, tt := range tests {
		got, err := d.FirstSeen(ctx, tt.id)
		if err != nil {
			t.Fatalf("FirstSeen(%q) unexpected error: %v", tt.id, err)
		}
		if got != tt.want {
			t.Errorf("FirstSeen(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

//nolint:revive // Test function signature requires *testing.T even when unused
func TestInstrumentedStore_DurationLabels(t *testing.T) {
	duration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "test_labels_duration"},
		[]string{"backend", "operation"},
	)
	errCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "test_labels_errors"},
		[]string{"backend", "operation"},
	)
	inner := newMemoryStore(config.StoreConfig{}, Options{})
	is := NewInstrumentedStore(inner, duration, errCounter)
	ctx := context.Background()

	_ = is.Ping(ctx)
	_ = is.Close()
	_, _ = is.Dedupe().FirstSeen(ctx, "x")
	_ = is.RunBuffer().Add(ctx, "u", "t", sampleEvent("t", domain.StateRunning))
	_, _, _ = is.RunBuffer().Flush(ctx, "u")
}
