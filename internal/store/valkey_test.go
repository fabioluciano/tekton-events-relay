package store

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func TestValkeyStore_FirstSeen(t *testing.T) {
	const evt1 = "evt-1"
	tests := []struct {
		name  string
		setup func(*miniredis.Miniredis)
		ids   []string
		want  []bool
	}{
		{
			name: "first call returns true",
			ids:  []string{evt1},
			want: []bool{true},
		},
		{
			name: "duplicate returns false",
			ids:  []string{"evt-1", "evt-1"},
			want: []bool{true, false},
		},
		{
			name: "different ids are all first",
			ids:  []string{"a", "b", "c"},
			want: []bool{true, true, true},
		},
		{
			name: "interleaved duplicates",
			ids:  []string{"x", "y", "x", "z"},
			want: []bool{true, true, false, true},
		},
		{
			name: "empty string id",
			ids:  []string{""},
			want: []bool{true},
		},
		{
			name:  "key already exists from outside",
			setup: func(mr *miniredis.Miniredis) { _ = mr.Set(defaultKeyPrefix+":dedupe:evt-x", "") },
			ids:   []string{"evt-x"},
			want:  []bool{false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			s, err := New(config.StoreConfig{
				Backend: BackendValkey,
				TTL:     time.Hour,
				Valkey:  config.ValkeyConfig{Address: mr.Addr()},
			}, Options{})
			if err != nil {
				t.Fatalf("new valkey store: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })

			if tt.setup != nil {
				tt.setup(mr)
			}

			ctx := context.Background()
			for i, id := range tt.ids {
				got, err := s.Dedupe().FirstSeen(ctx, id)
				if err != nil {
					t.Fatalf("FirstSeen(%q) unexpected error: %v", id, err)
				}
				if got != tt.want[i] {
					t.Errorf("FirstSeen(%q) call %d = %v, want %v", id, i, got, tt.want[i])
				}
			}
		})
	}
}

func TestValkeyStore_Add(t *testing.T) {
	const taskBuild = "build"
	tests := []struct {
		name  string
		tasks []struct {
			uid, task string
			state     domain.State
		}
		wantCount  int
		wantStates map[string]domain.State // final expected state per task name
	}{
		{
			name: "single task",
			tasks: []struct {
				uid, task string
				state     domain.State
			}{
				{uid: "uid-1", task: taskBuild, state: domain.StateRunning},
			},
			wantCount:  1,
			wantStates: map[string]domain.State{taskBuild: domain.StateRunning},
		},
		{
			name: "multiple tasks same run",
			tasks: []struct {
				uid, task string
				state     domain.State
			}{
				{uid: "uid-2", task: taskBuild, state: domain.StateRunning},
				{uid: "uid-2", task: "test", state: domain.StateSuccess},
				{uid: "uid-2", task: "lint", state: domain.StateFailure},
			},
			wantCount: 3,
			wantStates: map[string]domain.State{
				"build": domain.StateRunning,
				"test":  domain.StateSuccess,
				"lint":  domain.StateFailure,
			},
		},
		{
			name: "overwrite existing task",
			tasks: []struct {
				uid, task string
				state     domain.State
			}{
				{uid: "uid-3", task: taskBuild, state: domain.StateRunning},
				{uid: "uid-3", task: taskBuild, state: domain.StateSuccess},
			},
			wantCount:  1,
			wantStates: map[string]domain.State{taskBuild: domain.StateSuccess},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			s, err := New(config.StoreConfig{
				Backend: BackendValkey,
				TTL:     time.Hour,
				Valkey:  config.ValkeyConfig{Address: mr.Addr()},
			}, Options{})
			if err != nil {
				t.Fatalf("new valkey store: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })

			ctx := context.Background()
			for _, a := range tt.tasks {
				if err := s.RunBuffer().Add(ctx, a.uid, a.task, sampleEvent(a.task, a.state)); err != nil {
					t.Fatalf("Add(%q, %q): %v", a.uid, a.task, err)
				}
			}

			uid := tt.tasks[0].uid
			tasks, found, err := s.RunBuffer().Flush(ctx, uid)
			if err != nil {
				t.Fatalf("Flush(%q): %v", uid, err)
			}
			if !found {
				t.Fatal("Flush returned not found")
			}
			if len(tasks) != tt.wantCount {
				t.Errorf("len(tasks) = %d, want %d", len(tasks), tt.wantCount)
			}
			for task, wantState := range tt.wantStates {
				ev, ok := tasks[task]
				if !ok {
					t.Errorf("task %q not found in flushed result", task)
					continue
				}
				if ev.State != wantState {
					t.Errorf("task %q state = %q, want %q", task, ev.State, wantState)
				}
			}
		})
	}
}

func TestValkeyStore_Flush(t *testing.T) {
	mr := miniredis.RunT(t)
	s, err := New(config.StoreConfig{
		Backend: BackendValkey,
		TTL:     time.Hour,
		Valkey:  config.ValkeyConfig{Address: mr.Addr()},
	}, Options{})
	if err != nil {
		t.Fatalf("new valkey store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	rb := s.RunBuffer()

	// Flush on non-existent key returns not found, no error.
	_, found, err := rb.Flush(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Flush nonexistent: %v", err)
	}
	if found {
		t.Error("Flush on nonexistent key should be false")
	}

	// Add data and flush.
	if err := rb.Add(ctx, "uid-1", "build", sampleEvent("build", domain.StateSuccess)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	tasks, found, err := rb.Flush(ctx, "uid-1")
	if err != nil {
		t.Fatalf("Flush uid-1: %v", err)
	}
	if !found {
		t.Fatal("Flush should return found for existing key")
	}
	if len(tasks) != 1 || tasks["build"].State != domain.StateSuccess {
		t.Errorf("unexpected tasks: %+v", tasks)
	}

	// After flush, the key is deleted.
	if _, found, _ := rb.Flush(ctx, "uid-1"); found {
		t.Error("second Flush should report not found (atomic delete via Lua)")
	}
}

func TestValkeyStore_TTL(t *testing.T) {
	tests := []struct {
		name    string
		ttl     time.Duration
		advance time.Duration
		wantTTL bool
	}{
		{
			name:    "key has TTL set after FirstSeen",
			ttl:     time.Hour,
			advance: 30 * time.Minute,
			wantTTL: true,
		},
		{
			name:    "key expires after TTL",
			ttl:     time.Second,
			advance: 2 * time.Second,
			wantTTL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			s, err := New(config.StoreConfig{
				Backend: BackendValkey,
				TTL:     tt.ttl,
				Valkey:  config.ValkeyConfig{Address: mr.Addr()},
			}, Options{})
			if err != nil {
				t.Fatalf("new valkey store: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })

			ctx := context.Background()

			_, err = s.Dedupe().FirstSeen(ctx, "evt-ttl")
			if err != nil {
				t.Fatalf("FirstSeen: %v", err)
			}
			mr.FastForward(tt.advance)
			key := defaultKeyPrefix + ":dedupe:evt-ttl"
			if tt.wantTTL {
				if ttl := mr.TTL(key); ttl <= 0 {
					t.Errorf("dedupe key TTL = %v, want > 0", ttl)
				}
				first, err := s.Dedupe().FirstSeen(ctx, "evt-ttl")
				if err != nil {
					t.Fatalf("second FirstSeen: %v", err)
				}
				if first {
					t.Error("second FirstSeen should be false (key still active)")
				}
			} else {
				if mr.Exists(key) {
					t.Error("dedupe key should have expired")
				}
				first, err := s.Dedupe().FirstSeen(ctx, "evt-ttl")
				if err != nil {
					t.Fatalf("FirstSeen after expiry: %v", err)
				}
				if !first {
					t.Error("FirstSeen after expiry should be true")
				}
			}

			runKey := defaultKeyPrefix + ":run:uid-ttl"
			if err := s.RunBuffer().Add(ctx, "uid-ttl", "build", sampleEvent("build", domain.StateRunning)); err != nil {
				t.Fatalf("Add: %v", err)
			}
			mr.FastForward(tt.advance)

			if tt.wantTTL {
				if ttl := mr.TTL(runKey); ttl <= 0 {
					t.Errorf("run buffer key TTL = %v, want > 0", ttl)
				}
			} else {
				if mr.Exists(runKey) {
					t.Error("run buffer key should have expired")
				}
			}
		})
	}
}

func TestValkeyStore_DedupeFailClosedOnError(t *testing.T) {
	mr := miniredis.RunT(t)
	s, err := New(config.StoreConfig{
		Backend: BackendValkey,
		TTL:     time.Hour,
		Valkey:  config.ValkeyConfig{Address: mr.Addr()},
	}, Options{})
	if err != nil {
		t.Fatalf("new valkey store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	mr.Close() // Close the underlying miniredis to simulate outage

	// Dedupe returns error when backend is down.
	_, err = s.Dedupe().FirstSeen(context.Background(), "any")
	if err == nil {
		t.Error("expected error when backend is down")
	}

	// Run buffer Add errors when backend is down.
	err = s.RunBuffer().Add(context.Background(), "uid", "task", sampleEvent("t", domain.StateRunning))
	if err == nil {
		t.Error("expected error when backend is down for Add")
	}

	// Run buffer Flush errors when backend is down.
	_, _, err = s.RunBuffer().Flush(context.Background(), "uid")
	if err == nil {
		t.Error("expected error when backend is down for Flush")
	}
}
