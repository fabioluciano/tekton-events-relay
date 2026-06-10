package store

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func sampleEvent(task string, state domain.State) *domain.Event {
	return &domain.Event{
		Resource: domain.ResourceTaskRun,
		RunName:  task,
		State:    state,
	}
}

func TestNew_UnknownBackend(t *testing.T) {
	_, err := New(config.StoreConfig{Backend: "etcd"}, Options{})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestNew_DefaultsToMemory(t *testing.T) {
	s, err := New(config.StoreConfig{}, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Backend() != BackendMemory {
		t.Errorf("backend = %q, want memory", s.Backend())
	}
}

func TestMemoryDedupe_FirstSeen(t *testing.T) {
	s := newMemoryStore(config.StoreConfig{}, Options{DedupeCapacity: 10})
	ctx := context.Background()

	first, err := s.Dedupe().FirstSeen(ctx, "evt-1")
	if err != nil || !first {
		t.Fatalf("FirstSeen = (%v, %v), want (true, nil)", first, err)
	}
	again, err := s.Dedupe().FirstSeen(ctx, "evt-1")
	if err != nil || again {
		t.Fatalf("FirstSeen repeat = (%v, %v), want (false, nil)", again, err)
	}
	if got := s.DedupeLen(); got != 1 {
		t.Errorf("DedupeLen = %d, want 1", got)
	}
}

func TestMemoryRunBuffer_AddFlushDeepCopies(t *testing.T) {
	s := newMemoryStore(config.StoreConfig{TTL: time.Minute}, Options{})
	ctx := context.Background()
	rb := s.RunBuffer()

	ev := sampleEvent("build", domain.StateRunning)
	if err := rb.Add(ctx, "uid-1", "build", ev); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Mutating the original after Add must not affect the stored copy.
	ev.State = domain.StateFailure

	tasks, found, err := rb.Flush(ctx, "uid-1")
	if err != nil || !found {
		t.Fatalf("Flush = (found=%v, %v), want (true, nil)", found, err)
	}
	if tasks["build"].State != domain.StateRunning {
		t.Errorf("stored state = %q, want running (deep copy)", tasks["build"].State)
	}

	if _, found, _ := rb.Flush(ctx, "uid-1"); found {
		t.Error("second Flush should report not found")
	}
}

func newTestValkey(t *testing.T, ttl time.Duration) (*miniredis.Miniredis, Store) {
	t.Helper()
	mr := miniredis.RunT(t)
	s, err := New(config.StoreConfig{
		Backend: BackendValkey,
		TTL:     ttl,
		Valkey:  config.ValkeyConfig{Address: mr.Addr()},
	}, Options{})
	if err != nil {
		t.Fatalf("new valkey store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return mr, s
}

func TestValkeyDedupe_FirstSeen(t *testing.T) {
	mr, s := newTestValkey(t, time.Hour)
	ctx := context.Background()

	first, err := s.Dedupe().FirstSeen(ctx, "evt-1")
	if err != nil || !first {
		t.Fatalf("FirstSeen = (%v, %v), want (true, nil)", first, err)
	}
	again, err := s.Dedupe().FirstSeen(ctx, "evt-1")
	if err != nil || again {
		t.Fatalf("FirstSeen repeat = (%v, %v), want (false, nil)", again, err)
	}

	// TTL must be set on the dedupe key.
	key := defaultKeyPrefix + ":dedupe:evt-1"
	if ttl := mr.TTL(key); ttl <= 0 {
		t.Errorf("dedupe key TTL = %v, want > 0", ttl)
	}
}

func TestValkeyRunBuffer_AddFlush(t *testing.T) {
	_, s := newTestValkey(t, time.Hour)
	ctx := context.Background()
	rb := s.RunBuffer()

	if err := rb.Add(ctx, "uid-1", "build", sampleEvent("build", domain.StateSuccess)); err != nil {
		t.Fatalf("Add build: %v", err)
	}
	if err := rb.Add(ctx, "uid-1", "test", sampleEvent("test", domain.StateFailure)); err != nil {
		t.Fatalf("Add test: %v", err)
	}

	tasks, found, err := rb.Flush(ctx, "uid-1")
	if err != nil || !found {
		t.Fatalf("Flush = (found=%v, %v), want (true, nil)", found, err)
	}
	if len(tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(tasks))
	}
	if tasks["build"].State != domain.StateSuccess || tasks["test"].State != domain.StateFailure {
		t.Errorf("unexpected task states: %+v", tasks)
	}

	if _, found, _ := rb.Flush(ctx, "uid-1"); found {
		t.Error("second Flush should report not found (atomic delete)")
	}
}

func TestValkeyDedupe_FailsClosedWithError(t *testing.T) {
	mr, s := newTestValkey(t, time.Hour)
	mr.Close() // simulate backend outage

	_, err := s.Dedupe().FirstSeen(context.Background(), "evt-1")
	if err == nil {
		t.Fatal("expected error when backend is down (callers fail open on it)")
	}
}
