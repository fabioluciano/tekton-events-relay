package batch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// mockFlusher implements Flusher for testing.
type mockFlusher struct {
	mu     sync.Mutex
	calls  [][]domain.Event
	err    error
	called chan struct{}
}

func newMockFlusher() *mockFlusher {
	return &mockFlusher{called: make(chan struct{}, 100)}
}

func (m *mockFlusher) Flush(_ context.Context, events []domain.Event) error {
	m.mu.Lock()
	m.calls = append(m.calls, events)
	m.mu.Unlock()
	select {
	case m.called <- struct{}{}:
	default:
	}
	return m.err
}

func (m *mockFlusher) Calls() [][]domain.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([][]domain.Event, len(m.calls))
	copy(cp, m.calls)
	return cp
}

func (m *mockFlusher) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// mockDLQ implements DLQEnqueuer for testing.
type mockDLQ struct {
	mu     sync.Mutex
	events []dlqEntry
}

type dlqEntry struct {
	cloudEventID string
	event        domain.Event
	cause        error
}

func (m *mockDLQ) Enqueue(_ context.Context, env *event.Envelope, cause error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, dlqEntry{
		cloudEventID: env.CloudEventID,
		event:        env.Report,
		cause:        cause,
	})
	return nil
}

func (m *mockDLQ) Events() []dlqEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]dlqEntry, len(m.events))
	copy(cp, m.events)
	return cp
}

func testEvent(name string) domain.Event {
	return domain.Event{
		RunName:   name,
		RunID:     "run-" + name,
		Namespace: "default",
		State:     domain.StateSuccess,
	}
}

func TestBatch_FlushOnMaxSize(t *testing.T) {
	f := newMockFlusher()
	cfg := Config{Enabled: true, MaxSize: 3, FlushInterval: time.Hour}
	h := New(f, "test", cfg, zap.NewNop(), nil)

	for i := 0; i < 3; i++ {
		if err := h.Handle(context.Background(), testEvent("evt")); err != nil {
			t.Fatalf("Handle failed: %v", err)
		}
	}

	// Wait for flush
	select {
	case <-f.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for flush")
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 flush call, got %d", len(calls))
	}
	if len(calls[0]) != 3 {
		t.Fatalf("expected 3 events in batch, got %d", len(calls[0]))
	}

	_ = h.Close()
}

func TestBatching(t *testing.T) {
	f := newMockFlusher()
	cfg := Config{Enabled: true, MaxSize: 100, FlushInterval: 100 * time.Millisecond}
	h := New(f, "test", cfg, zap.NewNop(), nil)

	if err := h.Handle(context.Background(), testEvent("evt1")); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if err := h.Handle(context.Background(), testEvent("evt2")); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	select {
	case <-f.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for interval flush")
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 flush call, got %d", len(calls))
	}
	if len(calls[0]) != 2 {
		t.Fatalf("expected 2 events in batch, got %d", len(calls[0]))
	}

	_ = h.Close()
}

func TestBatchFlushOnClose(t *testing.T) {
	f := newMockFlusher()
	cfg := Config{Enabled: true, MaxSize: 100, FlushInterval: time.Hour}
	h := New(f, "test", cfg, zap.NewNop(), nil)

	if err := h.Handle(context.Background(), testEvent("evt1")); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if err := h.Handle(context.Background(), testEvent("evt2")); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if err := h.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 flush call from Close, got %d", len(calls))
	}
	if len(calls[0]) != 2 {
		t.Fatalf("expected 2 events in final batch, got %d", len(calls[0]))
	}
}

func TestBatch_NoFlushWhenEmpty(t *testing.T) {
	f := newMockFlusher()
	cfg := Config{Enabled: true, MaxSize: 3, FlushInterval: 100 * time.Millisecond}
	h := New(f, "test", cfg, zap.NewNop(), nil)

	time.Sleep(200 * time.Millisecond)

	if err := h.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if f.CallCount() != 0 {
		t.Fatalf("expected 0 flush calls for empty buffer, got %d", f.CallCount())
	}
}

func TestBatch_DLQOnFlushFailure(t *testing.T) {
	f := newMockFlusher()
	f.err = errors.New("flush failed")
	dlq := &mockDLQ{}
	cfg := Config{Enabled: true, MaxSize: 2, FlushInterval: time.Hour}
	h := New(f, "test", cfg, zap.NewNop(), dlq)

	if err := h.Handle(context.Background(), testEvent("evt1")); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if err := h.Handle(context.Background(), testEvent("evt2")); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Wait for flush attempt
	select {
	case <-f.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for flush")
	}

	// Give DLQ time to process
	time.Sleep(100 * time.Millisecond)

	events := dlq.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 DLQ entries, got %d", len(events))
	}

	_ = h.Close()
}

func TestBatch_CloseIdempotent(t *testing.T) {
	f := newMockFlusher()
	cfg := Config{Enabled: true, MaxSize: 100, FlushInterval: time.Hour}
	h := New(f, "test", cfg, zap.NewNop(), nil)

	if err := h.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

func TestBatch_HandleAfterClose(t *testing.T) {
	f := newMockFlusher()
	cfg := Config{Enabled: true, MaxSize: 100, FlushInterval: time.Hour}
	h := New(f, "test", cfg, zap.NewNop(), nil)

	_ = h.Close()

	err := h.Handle(context.Background(), testEvent("evt"))
	if err == nil {
		t.Fatal("expected error from Handle after Close")
	}
}

func TestBatch_MultipleFlushCycles(t *testing.T) {
	f := newMockFlusher()
	cfg := Config{Enabled: true, MaxSize: 2, FlushInterval: time.Hour}
	h := New(f, "test", cfg, zap.NewNop(), nil)

	// First batch
	_ = h.Handle(context.Background(), testEvent("a1"))
	_ = h.Handle(context.Background(), testEvent("a2"))

	select {
	case <-f.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first flush")
	}

	// Second batch
	_ = h.Handle(context.Background(), testEvent("b1"))
	_ = h.Handle(context.Background(), testEvent("b2"))

	select {
	case <-f.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second flush")
	}

	calls := f.Calls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 flush calls, got %d", len(calls))
	}

	_ = h.Close()
}

func TestBatch_ConcurrentHandle(t *testing.T) {
	f := newMockFlusher()
	cfg := Config{Enabled: true, MaxSize: 5, FlushInterval: 50 * time.Millisecond}
	h := New(f, "test", cfg, zap.NewNop(), nil)

	var wg sync.WaitGroup
	var count atomic.Int64
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.Handle(context.Background(), testEvent("concurrent")); err == nil {
				count.Add(1)
			}
		}()
	}
	wg.Wait()

	_ = h.Close()

	totalFlushed := 0
	for _, call := range f.Calls() {
		totalFlushed += len(call)
	}

	if totalFlushed != int(count.Load()) {
		t.Fatalf("expected %d total events flushed, got %d", count.Load(), totalFlushed)
	}
}

func TestBatch_NameAndType(t *testing.T) {
	f := newMockFlusher()
	cfg := Config{Enabled: true, MaxSize: 1, FlushInterval: time.Second}
	h := New(f, "slack", cfg, zap.NewNop(), nil)
	defer func() { _ = h.Close() }()

	if h.Name() != "slack" {
		t.Fatalf("expected name 'slack', got %q", h.Name())
	}
	if h.Type() != "notify" {
		t.Fatalf("expected type 'notify', got %q", h.Type())
	}
}
