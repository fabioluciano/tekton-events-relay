package dlq

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

func testEnvelope(id string) *event.Envelope {
	return &event.Envelope{
		CloudEventID:   id,
		CloudEventType: "dev.tekton.event.pipelinerun.failed.v1",
		Report: domain.Event{
			Provider: "github",
			Resource: domain.ResourcePipelineRun,
			RunName:  "run-" + id,
			State:    domain.StateFailure,
		},
	}
}

func newTestQueue(t *testing.T, maxBytes int64) *FileQueue {
	t.Helper()
	q, err := NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), maxBytes, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	return q
}

func TestFileQueue_EnqueueListRemove(t *testing.T) {
	q := newTestQueue(t, 0)
	ctx := context.Background()

	if err := q.Enqueue(ctx, testEnvelope("evt-1"), errors.New("token expired")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := q.Enqueue(ctx, testEnvelope("evt-2"), errors.New("repo not found")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	entries, err := q.List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].ID != "evt-1" || entries[0].Cause != "token expired" {
		t.Errorf("unexpected first entry: %+v", entries[0])
	}
	if entries[0].Envelope.Report.RunName != "run-evt-1" {
		t.Errorf("envelope not round-tripped: %+v", entries[0].Envelope)
	}

	if err := q.Remove(ctx, "evt-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	size, err := q.Size(ctx)
	if err != nil || size != 1 {
		t.Errorf("Size = (%d, %v), want (1, nil)", size, err)
	}
}

func TestFileQueue_ReEnqueueBumpsRetryCount(t *testing.T) {
	q := newTestQueue(t, 0)
	ctx := context.Background()

	for range 3 {
		if err := q.Enqueue(ctx, testEnvelope("evt-1"), errors.New("still broken")); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	entries, _ := q.List(ctx, 0)
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 (replaced, not appended)", len(entries))
	}
	if entries[0].RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2", entries[0].RetryCount)
	}
}

func TestFileQueue_ListLimit(t *testing.T) {
	q := newTestQueue(t, 0)
	ctx := context.Background()

	for i := range 5 {
		_ = q.Enqueue(ctx, testEnvelope(fmt.Sprintf("evt-%d", i)), errors.New("x"))
	}
	entries, _ := q.List(ctx, 2)
	if len(entries) != 2 {
		t.Errorf("len(entries) = %d, want 2", len(entries))
	}
}

func TestFileQueue_SizeBoundDropsOldest(t *testing.T) {
	// Each encoded entry is a few hundred bytes; a 1KB bound forces drops.
	q := newTestQueue(t, 1024)
	ctx := context.Background()

	for i := range 20 {
		if err := q.Enqueue(ctx, testEnvelope(fmt.Sprintf("evt-%02d", i)), errors.New("x")); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	entries, err := q.List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) == 0 || len(entries) >= 20 {
		t.Fatalf("len(entries) = %d, want bounded between 1 and 19", len(entries))
	}
	// Oldest must have been dropped, newest kept.
	if entries[len(entries)-1].ID != "evt-19" {
		t.Errorf("newest entry = %s, want evt-19", entries[len(entries)-1].ID)
	}
	if entries[0].ID == "evt-00" {
		t.Error("oldest entry should have been dropped by the size bound")
	}
}

func TestFileQueue_SurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dlq.jsonl")

	q1, err := NewFileQueue(path, 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	_ = q1.Enqueue(context.Background(), testEnvelope("evt-1"), errors.New("x"))

	q2, err := NewFileQueue(path, 0, 0)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	size, _ := q2.Size(context.Background())
	if size != 1 {
		t.Errorf("Size after reopen = %d, want 1", size)
	}
}

func TestFileQueue_EnqueuePropagatesWriteError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dlq.jsonl")

	q, err := NewFileQueue(path, 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}

	_ = os.RemoveAll(dir)

	err = q.Enqueue(context.Background(), testEnvelope("evt-1"), errors.New("x"))
	if err == nil {
		t.Fatal("expected error on Enqueue after directory removal, got nil")
	}
}

func TestFileQueue_RemovePropagatesWriteError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dlq.jsonl")

	q, err := NewFileQueue(path, 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	_ = q.Enqueue(context.Background(), testEnvelope("evt-1"), errors.New("x"))
	_ = q.Enqueue(context.Background(), testEnvelope("evt-2"), errors.New("y"))

	_ = os.RemoveAll(dir)

	err = q.Remove(context.Background(), "evt-1")
	if err == nil {
		t.Fatal("expected error on Remove after directory removal, got nil")
	}
}

func TestFileQueue_RenameFailurePropagated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dlq.jsonl")

	q, err := NewFileQueue(path, 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	_ = q.Enqueue(context.Background(), testEnvelope("evt-1"), errors.New("x"))

	_ = os.Remove(path)
	if err := os.Mkdir(path, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	err = q.Enqueue(context.Background(), testEnvelope("evt-2"), errors.New("y"))
	if err == nil {
		t.Fatal("expected error on Enqueue when destination is a directory, got nil")
	}
}

func testEnvelopeWithProvider(id, provider string, state domain.State) *event.Envelope {
	return &event.Envelope{
		CloudEventID:   id,
		CloudEventType: "dev.tekton.event.pipelinerun.failed.v1",
		Report: domain.Event{
			Provider: provider,
			Resource: domain.ResourcePipelineRun,
			RunName:  "run-" + id,
			State:    state,
		},
	}
}

func TestScanMethod(t *testing.T) {
	t.Run("iterates all entries", func(t *testing.T) {
		q := newTestQueue(t, 0)
		ctx := context.Background()

		for i := range 5 {
			_ = q.Enqueue(ctx, testEnvelope(fmt.Sprintf("evt-%d", i)), errors.New("x"))
		}

		var scanned []string
		err := q.Scan(ctx, func(e DeadEvent) error {
			scanned = append(scanned, e.ID)
			return nil
		})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(scanned) != 5 {
			t.Errorf("scanned %d entries, want 5", len(scanned))
		}
	})

	t.Run("stops on callback error", func(t *testing.T) {
		q := newTestQueue(t, 0)
		ctx := context.Background()

		for i := range 5 {
			_ = q.Enqueue(ctx, testEnvelope(fmt.Sprintf("evt-%d", i)), errors.New("x"))
		}

		stopErr := errors.New("enough")
		count := 0
		err := q.Scan(ctx, func(_ DeadEvent) error {
			count++
			if count == 2 {
				return stopErr
			}
			return nil
		})
		if !errors.Is(err, stopErr) {
			t.Errorf("Scan error = %v, want %v", err, stopErr)
		}
		if count != 2 {
			t.Errorf("count = %d, want 2", count)
		}
	})

	t.Run("stops early with ErrStopScan", func(t *testing.T) {
		q := newTestQueue(t, 0)
		ctx := context.Background()

		for i := range 5 {
			_ = q.Enqueue(ctx, testEnvelope(fmt.Sprintf("evt-%d", i)), errors.New("x"))
		}

		count := 0
		err := q.Scan(ctx, func(_ DeadEvent) error {
			count++
			if count == 3 {
				return ErrStopScan
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if count != 3 {
			t.Errorf("count = %d, want 3", count)
		}
	})

	t.Run("empty file returns nil", func(t *testing.T) {
		q := newTestQueue(t, 0)
		ctx := context.Background()

		var called bool
		err := q.Scan(ctx, func(_ DeadEvent) error {
			called = true
			return nil
		})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if called {
			t.Error("callback called on empty file")
		}
	})

	t.Run("non-existent file returns nil", func(t *testing.T) {
		dir := t.TempDir()
		q, err := NewFileQueue(filepath.Join(dir, "missing.jsonl"), 0, 0)
		if err != nil {
			t.Fatalf("NewFileQueue: %v", err)
		}

		err = q.Scan(context.Background(), func(_ DeadEvent) error {
			return nil
		})
		if err != nil {
			t.Fatalf("Scan on missing file: %v", err)
		}
	})
}

func TestReplayFilter_Matches(t *testing.T) {
	base := DeadEvent{
		ID:       "evt-1",
		FailedAt: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		Envelope: testEnvelopeWithProvider("evt-1", "github", domain.StateFailure),
	}

	t.Run("empty filter matches all", func(t *testing.T) {
		f := ReplayFilter{}
		if !f.Matches(base) {
			t.Error("empty filter should match")
		}
	})

	t.Run("provider match", func(t *testing.T) {
		f := ReplayFilter{Provider: "github"}
		if !f.Matches(base) {
			t.Error("should match github")
		}
	})

	t.Run("provider mismatch", func(t *testing.T) {
		f := ReplayFilter{Provider: "gitlab"}
		if f.Matches(base) {
			t.Error("should not match gitlab")
		}
	})

	t.Run("state match", func(t *testing.T) {
		f := ReplayFilter{State: "failure"}
		if !f.Matches(base) {
			t.Error("should match failure")
		}
	})

	t.Run("state mismatch", func(t *testing.T) {
		f := ReplayFilter{State: "success"}
		if f.Matches(base) {
			t.Error("should not match success")
		}
	})

	t.Run("after before failed_at matches", func(t *testing.T) {
		f := ReplayFilter{After: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
		if !f.Matches(base) {
			t.Error("should match when after is before failed_at")
		}
	})

	t.Run("after after failed_at does not match", func(t *testing.T) {
		f := ReplayFilter{After: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)}
		if f.Matches(base) {
			t.Error("should not match when after is after failed_at")
		}
	})

	t.Run("combined filters", func(t *testing.T) {
		f := ReplayFilter{
			Provider: "github",
			State:    "failure",
			After:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		if !f.Matches(base) {
			t.Error("should match all combined filters")
		}
	})

	t.Run("nil envelope rejects provider filter", func(t *testing.T) {
		e := DeadEvent{ID: "evt-nil", Envelope: nil}
		f := ReplayFilter{Provider: "github"}
		if f.Matches(e) {
			t.Error("nil envelope should not match provider filter")
		}
	})
}

func TestRetentionPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dlq.jsonl")

	q, err := NewFileQueue(path, 0, 7)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()

	oldEntry := DeadEvent{
		ID:         "old-entry",
		FailedAt:   time.Now().UTC().AddDate(0, 0, -10),
		Cause:      "old error",
		Envelope:   testEnvelope("old-entry"),
		RetryCount: 0,
	}
	recentEntry := DeadEvent{
		ID:         "recent-entry",
		FailedAt:   time.Now().UTC().AddDate(0, 0, -1),
		Cause:      "recent error",
		Envelope:   testEnvelope("recent-entry"),
		RetryCount: 0,
	}

	entries := []DeadEvent{oldEntry, recentEntry}
	if err := q.writeAll(entries); err != nil {
		t.Fatalf("writeAll: %v", err)
	}

	q.purgeExpired()

	remaining, err := q.List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("len(remaining) = %d, want 1", len(remaining))
	}
	if remaining[0].ID != "recent-entry" {
		t.Errorf("remaining ID = %s, want recent-entry", remaining[0].ID)
	}
}

func TestRetentionPolicy_KeepsRecentEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dlq.jsonl")

	q, err := NewFileQueue(path, 0, 7)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	defer func() { _ = q.Close() }()

	ctx := context.Background()

	for i := range 3 {
		_ = q.Enqueue(ctx, testEnvelope(fmt.Sprintf("evt-%d", i)), errors.New("x"))
	}

	q.purgeExpired()

	remaining, err := q.List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(remaining) != 3 {
		t.Errorf("len(remaining) = %d, want 3", len(remaining))
	}
}

func TestRetentionPolicy_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dlq.jsonl")

	q, err := NewFileQueue(path, 0, 7)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	defer func() { _ = q.Close() }()

	q.purgeExpired()

	size, err := q.Size(context.Background())
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != 0 {
		t.Errorf("Size = %d, want 0", size)
	}
}

func TestClose_StopsRetention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dlq.jsonl")

	q, err := NewFileQueue(path, 0, 7)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_ = q.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return within 2s")
	}
}
