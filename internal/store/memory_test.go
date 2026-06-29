package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func TestMemoryStore_FirstSeen_Dedupe(t *testing.T) {
	const evt1 = "evt-1"
	tests := []struct {
		name     string
		ids      []string
		want     []bool // firstSeen results for each id
		wantSize int
	}{
		{
			name:     "single id returns true once",
			ids:      []string{evt1},
			want:     []bool{true},
			wantSize: 1,
		},
		{
			name:     "duplicate id returns false",
			ids:      []string{"evt-1", "evt-1"},
			want:     []bool{true, false},
			wantSize: 1,
		},
		{
			name:     "empty string id is accepted",
			ids:      []string{""},
			want:     []bool{true},
			wantSize: 1,
		},
		{
			name:     "many unique ids are all first seen",
			ids:      []string{"a", "b", "c", "d", "e"},
			want:     []bool{true, true, true, true, true},
			wantSize: 5,
		},
		{
			name:     "interleaved duplicates",
			ids:      []string{"x", "y", "x", "z", "y"},
			want:     []bool{true, true, false, true, false},
			wantSize: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newMemoryStore(config.StoreConfig{}, Options{DedupeCapacity: 100})
			ctx := context.Background()

			for i, id := range tt.ids {
				got, err := s.Dedupe().FirstSeen(ctx, id)
				if err != nil {
					t.Fatalf("FirstSeen(%q) unexpected error: %v", id, err)
				}
				if got != tt.want[i] {
					t.Errorf("FirstSeen(%q) = %v, want %v (call %d)", id, got, tt.want[i], i)
				}
			}
			if got := s.DedupeLen(); got != tt.wantSize {
				t.Errorf("DedupeLen = %d, want %d", got, tt.wantSize)
			}
		})
	}
}

func TestMemoryStore_Close(t *testing.T) {
	s := newMemoryStore(config.StoreConfig{}, Options{})
	if err := s.Close(); err != nil {
		t.Fatalf("Close() unexpected error: %v", err)
	}
	// Double close should also succeed for memory store.
	if err := s.Close(); err != nil {
		t.Fatalf("second Close() unexpected error: %v", err)
	}
}

func TestMemoryStore_Add_Eviction(t *testing.T) {
	// Buffer capacity of 2 means the third addition evicts the oldest.
	s := newMemoryStore(config.StoreConfig{TTL: time.Minute}, Options{BufferCapacity: 2})
	ctx := context.Background()
	rb := s.RunBuffer()

	if err := rb.Add(ctx, "uid-1", "build", sampleEvent("build", domain.StateRunning)); err != nil {
		t.Fatalf("Add uid-1: %v", err)
	}
	if err := rb.Add(ctx, "uid-2", "test", sampleEvent("test", domain.StateRunning)); err != nil {
		t.Fatalf("Add uid-2: %v", err)
	}
	// Adding a third entry with capacity 2 evicts uid-1 (oldest).
	if err := rb.Add(ctx, "uid-3", "lint", sampleEvent("lint", domain.StateRunning)); err != nil {
		t.Fatalf("Add uid-3: %v", err)
	}

	// uid-1 was evicted.
	if _, found, err := rb.Flush(ctx, "uid-1"); err != nil {
		t.Fatalf("Flush uid-1: %v", err)
	} else if found {
		t.Error("uid-1 should have been evicted")
	}

	// uid-2 and uid-3 still present.
	tasks2, found, err := rb.Flush(ctx, "uid-2")
	if err != nil {
		t.Fatalf("Flush uid-2: %v", err)
	}
	if !found {
		t.Error("uid-2 should still be present")
	}
	if tasks2["test"].State != domain.StateRunning {
		t.Errorf("uid-2 state = %q, want running", tasks2["test"].State)
	}

	tasks3, found, err := rb.Flush(ctx, "uid-3")
	if err != nil {
		t.Fatalf("Flush uid-3: %v", err)
	}
	if !found {
		t.Error("uid-3 should still be present")
	}
	if tasks3["lint"].State != domain.StateRunning {
		t.Errorf("uid-3 state = %q, want running", tasks3["lint"].State)
	}
}

func TestMemoryStore_Add_TTLExpiry(t *testing.T) {
	s := newMemoryStore(config.StoreConfig{TTL: 100 * time.Millisecond}, Options{BufferCapacity: 10})
	ctx := context.Background()
	rb := s.RunBuffer()

	if err := rb.Add(ctx, "uid-1", "build", sampleEvent("build", domain.StateRunning)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Immediately flush should find it.
	_, found, err := rb.Flush(ctx, "uid-1")
	if err != nil {
		t.Fatalf("immediate Flush: %v", err)
	}
	if !found {
		t.Error("immediate Flush should find the entry")
	}

	// Re-add since flush removes it.
	if err := rb.Add(ctx, "uid-1", "build", sampleEvent("build", domain.StateRunning)); err != nil {
		t.Fatalf("Add after flush: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	// Trigger lazy eviction by adding another entry.
	if err := rb.Add(ctx, "uid-2", "deploy", sampleEvent("deploy", domain.StateRunning)); err != nil {
		t.Fatalf("Add uid-2 after TTL: %v", err)
	}

	// uid-1 should have expired.
	if _, found, err := rb.Flush(ctx, "uid-1"); err != nil {
		t.Fatalf("Flush expired: %v", err)
	} else if found {
		t.Error("uid-1 should have expired due to TTL")
	}
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	s := newMemoryStore(config.StoreConfig{TTL: time.Minute}, Options{DedupeCapacity: 1000, BufferCapacity: 100})
	ctx := context.Background()

	var wg sync.WaitGroup
	n := 50

	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("evt-%c%c", 'A'+idx%26, '0'+idx/26)
			_, _ = s.Dedupe().FirstSeen(ctx, id)
		}(i)
	}

	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			uid := fmt.Sprintf("uid-%c", 'A'+idx%10)
			task := fmt.Sprintf("task-%c", '0'+idx)
			_ = s.RunBuffer().Add(ctx, uid, task, sampleEvent(task, domain.StateRunning))
		}(i)
	}

	wg.Wait()

	// Verify no panics or corruption — flush each uid that was written.
	for i := range 10 {
		uid := "uid-" + string(rune('A'+i))
		_, found, err := s.RunBuffer().Flush(ctx, uid)
		if err != nil {
			t.Errorf("Flush %s: %v", uid, err)
		}
		if !found {
			// Some UIDs may have been evicted if capacity was exceeded,
			// but at least the operation didn't panic or corrupt.
			t.Logf("Flush %s: not found (may have been evicted)", uid)
		}
	}
}
