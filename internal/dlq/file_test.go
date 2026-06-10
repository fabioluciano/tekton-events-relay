package dlq

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

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
	q, err := NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), maxBytes)
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

	q1, err := NewFileQueue(path, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	_ = q1.Enqueue(context.Background(), testEnvelope("evt-1"), errors.New("x"))

	q2, err := NewFileQueue(path, 0)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	size, _ := q2.Size(context.Background())
	if size != 1 {
		t.Errorf("Size after reopen = %d, want 1", size)
	}
}
