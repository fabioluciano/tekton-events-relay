package accumulator

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	testUID       = "uid-1"
	testTaskBuild = "task-build"
	testTaskTest  = "task-test"
)

// waitForCondition polls cond every 10ms until it returns true or the timeout expires.
// Uses time.NewTicker + time.After for deterministic synchronization without blocking sleeps.
func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(timeout)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("condition not met within %v", timeout)
		case <-ticker.C:
		}
	}
}

func makeEvent(runName string) *domain.Event {
	return &domain.Event{
		RunName:  runName,
		Resource: domain.ResourceTaskRun,
		State:    domain.StateRunning,
	}
}

func TestNewLRUBuffer(t *testing.T) {
	ctx := context.Background()

	t.Run("usable with zero values (defaults applied)", func(t *testing.T) {
		buf := NewLRUBuffer(0, 0)
		defer func() { _ = buf.Close() }()
		buf.Add(ctx, testUID, makeEvent("task-a"))
		_, ok := buf.Get(testUID)
		if !ok {
			t.Error("expected entry to exist after Add")
		}
	})

	t.Run("usable with explicit values", func(t *testing.T) {
		buf := NewLRUBuffer(5*time.Second, 50)
		defer func() { _ = buf.Close() }()
		buf.Add(ctx, testUID, makeEvent("task-a"))
		_, ok := buf.Get(testUID)
		if !ok {
			t.Error("expected entry to exist after Add")
		}
	})
}

func TestLRUBuffer_Add(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 100)
	defer func() { _ = buf.Close() }()

	buf.Add(ctx, "pipeline-uid-1", makeEvent(testTaskBuild))
	buf.Add(ctx, "pipeline-uid-1", makeEvent(testTaskTest))
	buf.Add(ctx, "pipeline-uid-2", makeEvent("task-deploy"))

	state1, ok := buf.Get("pipeline-uid-1")
	if !ok {
		t.Fatal("expected pipeline-uid-1 to exist")
	}
	if len(state1.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(state1.Tasks))
	}
	if state1.Tasks[testTaskBuild] == nil {
		t.Error("expected task-build to be present")
	}
	if state1.Tasks[testTaskTest] == nil {
		t.Error("expected task-test to be present")
	}

	state2, ok := buf.Get("pipeline-uid-2")
	if !ok {
		t.Fatal("expected pipeline-uid-2 to exist")
	}
	if len(state2.Tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(state2.Tasks))
	}
}

func TestLRUBuffer_Add_OverwritesSameTask(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 100)
	defer func() { _ = buf.Close() }()

	buf.Add(ctx, testUID, &domain.Event{RunName: "task-a", Resource: domain.ResourceTaskRun, State: domain.StateRunning})
	buf.Add(ctx, testUID, &domain.Event{RunName: "task-a", Resource: domain.ResourceTaskRun, State: domain.StateSuccess})

	state, _ := buf.Get(testUID)
	if state.Tasks["task-a"].State != domain.StateSuccess {
		t.Errorf("expected latest state Success, got %v", state.Tasks["task-a"].State)
	}
}

func TestLRUBuffer_Get(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 100)
	defer func() { _ = buf.Close() }()

	_, ok := buf.Get("nonexistent")
	if ok {
		t.Error("expected false for nonexistent UID")
	}

	buf.Add(ctx, testUID, makeEvent("task-a"))

	state, ok := buf.Get(testUID)
	if !ok {
		t.Fatal("expected uid-1 to exist after Add")
	}
	if state.UID != testUID {
		t.Errorf("expected UID uid-1, got %s", state.UID)
	}

	// Get does not remove
	_, ok = buf.Get(testUID)
	if !ok {
		t.Error("expected uid-1 to still exist after Get")
	}
}

func TestLRUBuffer_Flush(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 100)
	defer func() { _ = buf.Close() }()

	buf.Add(ctx, testUID, makeEvent("task-a"))
	buf.Add(ctx, testUID, makeEvent("task-b"))

	state, ok := buf.Flush(ctx, testUID)
	if !ok {
		t.Fatal("expected Flush to return true")
	}
	if len(state.Tasks) != 2 {
		t.Errorf("expected 2 tasks in flushed state, got %d", len(state.Tasks))
	}

	// Should be gone after flush
	_, ok = buf.Get(testUID)
	if ok {
		t.Error("expected uid-1 to be removed after Flush")
	}

	// Flush nonexistent
	_, ok = buf.Flush(ctx, "nonexistent")
	if ok {
		t.Error("expected false for flushing nonexistent UID")
	}
}

func TestLRUBuffer_TTLExpiry(t *testing.T) {
	ctx := context.Background()
	ttl := 100 * time.Millisecond
	buf := NewLRUBuffer(ttl, 100)
	defer func() { _ = buf.Close() }()

	buf.Add(ctx, testUID, makeEvent("task-a"))

	_, ok := buf.Get(testUID)
	if !ok {
		t.Fatal("expected uid-1 to exist immediately after Add")
	}

	waitForCondition(t, ttl+200*time.Millisecond, func() bool {
		_, found := buf.Get(testUID)
		return !found
	})
}

func TestLRUBuffer_MaxSize(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 3)
	defer func() { _ = buf.Close() }()

	buf.Add(ctx, testUID, makeEvent("task-a"))
	buf.Add(ctx, "uid-2", makeEvent("task-b"))
	buf.Add(ctx, "uid-3", makeEvent("task-c"))
	buf.Add(ctx, "uid-4", makeEvent("task-d"))

	_, ok := buf.Get(testUID)
	if ok {
		t.Error("expected uid-1 to be evicted (LRU)")
	}

	_, ok = buf.Get("uid-4")
	if !ok {
		t.Error("expected uid-4 to exist after eviction")
	}
}

func TestLRUBuffer_Concurrent(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	buf := NewLRUBuffer(5*time.Second, 1000)
	defer func() { _ = buf.Close() }()

	var wg sync.WaitGroup
	numGoroutines := 50
	eventsPerGoroutine := 20

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			uid := fmt.Sprintf("pipeline-%d", id%10)
			for j := 0; j < eventsPerGoroutine; j++ {
				event := makeEvent(fmt.Sprintf("task-%d-%d", id, j))
				buf.Add(ctx, uid, event)
			}
		}(i)
	}

	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer wg.Done()
			uid := fmt.Sprintf("pipeline-%d", id)
			for j := 0; j < 5; j++ {
				buf.Get(uid)
				buf.Flush(ctx, uid)
			}
		}(i)
	}

	wg.Wait()
}

func TestLRUBuffer_Concurrency(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(1*time.Minute, 100)
	defer func() { _ = buf.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			event := &domain.Event{
				Resource: domain.ResourceTaskRun,
				RunName:  fmt.Sprintf("task-%d", id),
			}
			buf.Add(ctx, "pipeline-1", event)
		}(i)
	}
	wg.Wait()

	state, ok := buf.Get("pipeline-1")
	if !ok {
		t.Fatal("expected state")
	}
	if len(state.Tasks) != 10 {
		t.Errorf("expected 10 tasks, got %d", len(state.Tasks))
	}
}

func TestLRUBuffer_Eviction(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(1*time.Hour, 3)
	defer func() { _ = buf.Close() }()

	for i := 1; i <= 5; i++ {
		event := &domain.Event{Resource: domain.ResourcePipelineRun}
		buf.Add(ctx, fmt.Sprintf("uid-%d", i), event)
	}

	count := 0
	for i := 1; i <= 5; i++ {
		if _, ok := buf.Get(fmt.Sprintf("uid-%d", i)); ok {
			count++
		}
	}
	if count != 3 {
		t.Errorf("expected 3 states after eviction, got %d", count)
	}

	_, ok1 := buf.Get(testUID)
	_, ok2 := buf.Get("uid-2")
	if ok1 || ok2 {
		t.Error("oldest entries should be evicted")
	}

	_, ok3 := buf.Get("uid-3")
	_, ok4 := buf.Get("uid-4")
	_, ok5 := buf.Get("uid-5")
	if !ok3 || !ok4 || !ok5 {
		t.Error("newest entries should remain after eviction")
	}
}

func TestLRUBuffer_TTLExpiryLoop(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(100*time.Millisecond, 100)
	defer func() { _ = buf.Close() }()

	event := &domain.Event{Resource: domain.ResourcePipelineRun}
	buf.Add(ctx, testUID, event)

	_, ok := buf.Get(testUID)
	if !ok {
		t.Fatal("expected state immediately after Add")
	}

	waitForCondition(t, 500*time.Millisecond, func() bool {
		_, found := buf.Get(testUID)
		return !found
	})
}

func TestLRUBuffer_AddWithGroup(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 100)
	defer func() { _ = buf.Close() }()

	buf.AddWithGroup(ctx, "group-1", testUID, makeEvent(testTaskBuild))
	buf.AddWithGroup(ctx, "group-1", testUID, makeEvent(testTaskTest))
	buf.AddWithGroup(ctx, "group-1", "uid-2", makeEvent("task-deploy"))

	state1, ok := buf.Get("group-1:uid-1")
	if !ok {
		t.Fatal("expected group-1:uid-1 to exist")
	}
	if len(state1.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(state1.Tasks))
	}

	state2, ok := buf.Get("group-1:uid-2")
	if !ok {
		t.Fatal("expected group-1:uid-2 to exist")
	}
	if len(state2.Tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(state2.Tasks))
	}
}

func TestLRUBuffer_AddWithGroup_EmptyGroupID(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 100)
	defer func() { _ = buf.Close() }()

	buf.AddWithGroup(ctx, "", testUID, makeEvent(testTaskBuild))

	_, ok := buf.Get(testUID)
	if !ok {
		t.Error("expected uid-1 to exist with empty groupID (backward compat)")
	}
}

func TestLRUBuffer_IsGroupComplete(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 100)
	defer func() { _ = buf.Close() }()

	if buf.IsGroupComplete("nonexistent") {
		t.Error("expected false for nonexistent group")
	}

	buf.AddWithGroup(ctx, "group-1", testUID, &domain.Event{
		Resource: domain.ResourcePipelineRun,
		RunName:  "pr-1",
		State:    domain.StateRunning,
	})
	if buf.IsGroupComplete("group-1") {
		t.Error("expected false when no members are terminal")
	}

	buf.AddWithGroup(ctx, "group-1", testUID, &domain.Event{
		Resource: domain.ResourcePipelineRun,
		RunName:  "pr-1",
		State:    domain.StateSuccess,
	})
	if !buf.IsGroupComplete("group-1") {
		t.Error("expected true when single member is terminal")
	}

	buf.AddWithGroup(ctx, "group-2", "uid-a", &domain.Event{
		Resource: domain.ResourcePipelineRun,
		RunName:  "pr-a",
		State:    domain.StateSuccess,
	})
	buf.AddWithGroup(ctx, "group-2", "uid-b", &domain.Event{
		Resource: domain.ResourcePipelineRun,
		RunName:  "pr-b",
		State:    domain.StateRunning,
	})
	if buf.IsGroupComplete("group-2") {
		t.Error("expected false when only one of two members is terminal")
	}

	buf.AddWithGroup(ctx, "group-2", "uid-b", &domain.Event{
		Resource: domain.ResourcePipelineRun,
		RunName:  "pr-b",
		State:    domain.StateFailure,
	})
	if !buf.IsGroupComplete("group-2") {
		t.Error("expected true when all members are terminal")
	}
}

func TestLRUBuffer_FlushGroup(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 100)
	defer func() { _ = buf.Close() }()

	buf.AddWithGroup(ctx, "group-1", testUID, &domain.Event{
		Resource: domain.ResourceTaskRun,
		RunName:  testTaskBuild,
		State:    domain.StateSuccess,
	})
	buf.AddWithGroup(ctx, "group-1", "uid-2", &domain.Event{
		Resource: domain.ResourceTaskRun,
		RunName:  testTaskTest,
		State:    domain.StateFailure,
	})

	state, ok := buf.FlushGroup(ctx, "group-1")
	if !ok {
		t.Fatal("expected FlushGroup to return true")
	}
	if len(state.Tasks) != 2 {
		t.Errorf("expected 2 tasks in merged state, got %d", len(state.Tasks))
	}
	if state.UID != "group-1" {
		t.Errorf("expected UID group-1, got %s", state.UID)
	}

	_, ok = buf.Get("group-1:uid-1")
	if ok {
		t.Error("expected group-1:uid-1 to be removed after FlushGroup")
	}
	_, ok = buf.Get("group-1:uid-2")
	if ok {
		t.Error("expected group-1:uid-2 to be removed after FlushGroup")
	}

	_, ok = buf.FlushGroup(ctx, "group-1")
	if ok {
		t.Error("expected false for flushing nonexistent group")
	}
}

func TestLRUBuffer_FlushGroup_MergesTaskOverwrites(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 100)
	defer func() { _ = buf.Close() }()

	buf.AddWithGroup(ctx, "group-1", testUID, &domain.Event{
		Resource: domain.ResourceTaskRun,
		RunName:  testTaskBuild,
		State:    domain.StateSuccess,
	})
	buf.AddWithGroup(ctx, "group-1", "uid-2", &domain.Event{
		Resource: domain.ResourceTaskRun,
		RunName:  testTaskTest,
		State:    domain.StateFailure,
	})
	buf.AddWithGroup(ctx, "group-1", testUID, &domain.Event{
		Resource: domain.ResourceTaskRun,
		RunName:  testTaskBuild,
		State:    domain.StateError,
	})

	state, ok := buf.FlushGroup(ctx, "group-1")
	if !ok {
		t.Fatal("expected FlushGroup to return true")
	}
	if len(state.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(state.Tasks))
	}
	if state.Tasks[testTaskBuild].State != domain.StateError {
		t.Errorf("expected latest state Error for task-build, got %v", state.Tasks[testTaskBuild].State)
	}
	if state.Tasks[testTaskTest].State != domain.StateFailure {
		t.Errorf("expected state Failure for task-test, got %v", state.Tasks[testTaskTest].State)
	}
}

func TestLRUBuffer_FlushGroup_EmptyTasks(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 100)
	defer func() { _ = buf.Close() }()

	buf.AddWithGroup(ctx, "group-1", testUID, &domain.Event{
		Resource: domain.ResourcePipelineRun,
		RunName:  "pr-1",
		State:    domain.StateRunning,
	})

	_, ok := buf.FlushGroup(ctx, "group-1")
	if ok {
		t.Error("expected false when group has no TaskRun tasks")
	}
}

func TestLRUBuffer_Close_ClearsGroups(t *testing.T) {
	ctx := context.Background()
	buf := NewLRUBuffer(10*time.Second, 100)

	buf.AddWithGroup(ctx, "group-1", testUID, makeEvent("task-a"))

	if err := buf.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	if buf.IsGroupComplete("group-1") {
		t.Error("expected groups to be cleared after Close")
	}
}
