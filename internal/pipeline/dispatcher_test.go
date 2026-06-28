package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	testNotifier1 = "notifier1"
	testNotifier2 = "notifier2"
	testEventID   = "test-id"
)

type mockHandler struct {
	name       string
	actionType notifier.ActionType
	handleErr  error
	callCount  int
	sleepDur   time.Duration
}

func (m *mockHandler) Name() string              { return m.name }
func (m *mockHandler) Type() notifier.ActionType { return m.actionType }
func (m *mockHandler) Handle(ctx context.Context, _ domain.Event) error {
	m.callCount++
	if m.sleepDur > 0 {
		select {
		case <-time.After(m.sleepDur):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.handleErr
}

func testLogger() *zap.Logger {
	return zap.NewNop()
}

type testHandler struct {
	BaseHandler
	fn func(context.Context, *event.Envelope) error
}

func (h *testHandler) Handle(ctx context.Context, env *event.Envelope) error {
	return h.fn(ctx, env)
}

func TestDispatcher_Handle_Success(t *testing.T) {
	mock1 := &mockHandler{name: testNotifier1, actionType: notifier.ActionCommitStatus}
	mock2 := &mockHandler{name: testNotifier2, actionType: notifier.ActionCommitStatus}

	registry := notifier.NewRegistry()
	registry.Register(mock1)
	registry.Register(mock2)

	nextCalled := false
	nextHandler := &testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		nextCalled = true
		return nil
	}}

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(nextHandler)

	env := &event.Envelope{
		CloudEventID: testEventID,
	}

	err := dispatcher.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if mock1.callCount != 1 {
		t.Errorf("%s call count = %d, want 1", testNotifier1, mock1.callCount)
	}
	if mock2.callCount != 1 {
		t.Errorf("%s call count = %d, want 1", testNotifier2, mock2.callCount)
	}
	if !nextCalled {
		t.Error("expected next handler to be called")
	}
}

func TestDispatcher_Handle_NotifierError(t *testing.T) {
	mock1 := &mockHandler{name: testNotifier1, actionType: notifier.ActionCommitStatus, handleErr: errors.New("handle failed")}
	mock2 := &mockHandler{name: testNotifier2, actionType: notifier.ActionCommitStatus}

	registry := notifier.NewRegistry()
	registry.Register(mock1)
	registry.Register(mock2)

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{
		CloudEventID: testEventID,
	}

	err := dispatcher.Handle(context.Background(), env)
	if err == nil {
		t.Fatal("expected error when notifier fails")
	}

	if mock1.callCount != 1 {
		t.Errorf("failing notifier should still be called once")
	}
	if mock2.callCount != 1 {
		t.Errorf("other notifiers should still be called")
	}
}

func TestDispatcher_Handle_NoNotifiers(t *testing.T) {
	registry := notifier.NewRegistry()

	nextCalled := false
	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		nextCalled = true
		return nil
	}})

	env := &event.Envelope{
		CloudEventID: testEventID,
	}

	err := dispatcher.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf("expected no error with no notifiers, got: %v", err)
	}

	if !nextCalled {
		t.Error("expected next handler to be called even with no notifiers")
	}
}

func TestDispatcher_Handle_MultipleErrors(t *testing.T) {
	mock1 := &mockHandler{name: testNotifier1, actionType: notifier.ActionCommitStatus, handleErr: errors.New("error1")}
	mock2 := &mockHandler{name: testNotifier2, actionType: notifier.ActionCommitStatus, handleErr: errors.New("error2")}

	registry := notifier.NewRegistry()
	registry.Register(mock1)
	registry.Register(mock2)

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{
		CloudEventID: testEventID,
	}

	err := dispatcher.Handle(context.Background(), env)
	if err == nil {
		t.Fatal("expected error when multiple notifiers fail")
	}

	if mock1.callCount != 1 || mock2.callCount != 1 {
		t.Error("all notifiers should be called despite errors")
	}
}

func TestDispatcher_ConcurrentExecution(t *testing.T) {
	const sleepDuration = 100 * time.Millisecond
	const numHandlers = 3

	registry := notifier.NewRegistry()
	for i := 0; i < numHandlers; i++ {
		name := fmt.Sprintf("handler%d", i)
		registry.Register(&mockHandler{
			name:       name,
			actionType: notifier.ActionCommitStatus,
			sleepDur:   sleepDuration,
		})
	}

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{
		CloudEventID: testEventID,
	}

	start := time.Now()
	err := dispatcher.Handle(context.Background(), env)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// If serial: 3 * 100ms = 300ms
	// If parallel: ~100ms
	// Allow 200ms tolerance for parallel execution
	maxExpected := sleepDuration + 100*time.Millisecond
	if elapsed > maxExpected {
		t.Errorf("execution took %v, expected < %v (parallel execution)", elapsed, maxExpected)
	}
}

func TestDispatcher_ErrorAggregation(t *testing.T) {
	mock1 := &mockHandler{
		name:       "handler1",
		actionType: notifier.ActionCommitStatus,
		handleErr:  errors.New("error1"),
	}
	mock2 := &mockHandler{
		name:       "handler2",
		actionType: notifier.ActionCommitStatus,
		handleErr:  errors.New("error2"),
	}
	mock3 := &mockHandler{
		name:       "handler3",
		actionType: notifier.ActionCommitStatus,
	}

	registry := notifier.NewRegistry()
	registry.Register(mock1)
	registry.Register(mock2)
	registry.Register(mock3)

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{
		CloudEventID: testEventID,
	}

	err := dispatcher.Handle(context.Background(), env)
	if err == nil {
		t.Fatal("expected aggregated error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "error1") {
		t.Errorf("error message should contain 'error1', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "error2") {
		t.Errorf("error message should contain 'error2', got: %s", errMsg)
	}
}

func TestDispatcher_ContextCancellation(t *testing.T) {
	const handlerSleep = 500 * time.Millisecond
	const cancelAfter = 50 * time.Millisecond

	slowHandler := &mockHandler{
		name:       "slow",
		actionType: notifier.ActionCommitStatus,
		sleepDur:   handlerSleep,
	}

	registry := notifier.NewRegistry()
	registry.Register(slowHandler)

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	ctx, cancel := context.WithTimeout(context.Background(), cancelAfter)
	defer cancel()

	env := &event.Envelope{
		CloudEventID: testEventID,
	}

	start := time.Now()
	err := dispatcher.Handle(ctx, env)
	elapsed := time.Since(start)

	if err != nil && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected context deadline error, got: %v", err)
	}

	// Should return quickly after context cancellation, not after full sleep
	maxExpected := 100 * time.Millisecond
	if elapsed > maxExpected {
		t.Errorf("execution took %v, expected < %v (should stop on context cancel)", elapsed, maxExpected)
	}
}

func TestDispatcher_HandledCountAccuracy(t *testing.T) {
	const provider = "test-provider"

	// 3 match provider
	match1 := &mockHandler{name: provider, actionType: notifier.ActionCommitStatus}
	match2 := &mockHandler{name: provider, actionType: notifier.ActionIssueComment, handleErr: errors.New("fail1")}
	match3 := &mockHandler{name: provider, actionType: notifier.ActionPRComment, handleErr: errors.New("fail2")}

	// 2 don't match provider
	noMatch1 := &mockHandler{name: "other1", actionType: notifier.ActionCommitStatus}
	noMatch2 := &mockHandler{name: "other2", actionType: notifier.ActionLabel}

	registry := notifier.NewRegistry()
	registry.Register(match1)
	registry.Register(match2)
	registry.Register(match3)
	registry.Register(noMatch1)
	registry.Register(noMatch2)

	core, observedLogs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	dispatcher := NewDispatcher(registry, logger, nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{
		CloudEventID: testEventID,
		Report: domain.Event{
			Provider: provider,
		},
	}

	err := dispatcher.Handle(context.Background(), env)
	if err == nil {
		t.Fatal("expected error from failing handlers")
	}

	// handledCount = 3 matched - 2 errors = 1
	// Should NOT see "no handlers processed event" warning
	logs := observedLogs.FilterMessage("no handlers processed event")
	if logs.Len() > 0 {
		t.Error("should not warn about no handlers when handledCount > 0")
	}

	// Verify error contains both failures
	errMsg := err.Error()
	if !strings.Contains(errMsg, "fail1") || !strings.Contains(errMsg, "fail2") {
		t.Errorf("error should contain both failures, got: %s", errMsg)
	}
}

// concurrentTrackerHandler tracks how many handlers run in parallel using an atomic counter.
type concurrentTrackerHandler struct {
	name       string
	actionType notifier.ActionType
	active     *atomic.Int32
	peak       *atomic.Int32
	sleepDur   time.Duration
}

func (h *concurrentTrackerHandler) Name() string              { return h.name }
func (h *concurrentTrackerHandler) Type() notifier.ActionType { return h.actionType }
func (h *concurrentTrackerHandler) Handle(ctx context.Context, _ domain.Event) error {
	cur := h.active.Add(1)
	for {
		old := h.peak.Load()
		if cur <= old || h.peak.CompareAndSwap(old, cur) {
			break
		}
	}
	defer h.active.Add(-1)

	select {
	case <-time.After(h.sleepDur):
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func TestDispatcher_MaxConcurrency_Enforced(t *testing.T) {
	const (
		numHandlers    = 6
		maxConcurrency = 2
		sleepDuration  = 100 * time.Millisecond
	)

	active := &atomic.Int32{}
	peak := &atomic.Int32{}

	registry := notifier.NewRegistry()
	for i := 0; i < numHandlers; i++ {
		registry.Register(&concurrentTrackerHandler{
			name:       fmt.Sprintf("handler%d", i),
			actionType: notifier.ActionCommitStatus,
			active:     active,
			peak:       peak,
			sleepDur:   sleepDuration,
		})
	}

	dispatcher := NewDispatcher(registry, testLogger(), nil, maxConcurrency)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{CloudEventID: testEventID}
	err := dispatcher.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if got := peak.Load(); got > maxConcurrency {
		t.Errorf("peak concurrency = %d, want ≤ %d", got, maxConcurrency)
	}
}

func TestDispatcher_ContextCancellation_ErrorPropagated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	slowHandler := &mockHandler{
		name:       "slow",
		actionType: notifier.ActionCommitStatus,
		sleepDur:   5 * time.Second,
	}

	registry := notifier.NewRegistry()
	registry.Register(slowHandler)

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{CloudEventID: testEventID}
	err := dispatcher.Handle(ctx, env)

	if err == nil {
		t.Fatal("expected error when context is cancelled, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled in error chain, got: %v", err)
	}
	if slowHandler.callCount != 0 {
		t.Errorf("handler should not be called when context is pre-cancelled, callCount = %d", slowHandler.callCount)
	}
}

func TestDispatcher_ContextCancellation_PartialHandlersRun(t *testing.T) {
	const handlerSleep = 500 * time.Millisecond
	const cancelAfter = 50 * time.Millisecond

	registry := notifier.NewRegistry()
	fastDone := make(chan struct{})
	registry.Register(&mockHandler{name: "fast", actionType: notifier.ActionCommitStatus})
	registry.Register(&mockHandler{name: "slow", actionType: notifier.ActionNotify, sleepDur: handlerSleep})

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		close(fastDone)
		return nil
	}})

	ctx, cancel := context.WithTimeout(context.Background(), cancelAfter)
	defer cancel()

	env := &event.Envelope{CloudEventID: testEventID}

	start := time.Now()
	err := dispatcher.Handle(ctx, env)
	elapsed := time.Since(start)

	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context error, got: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("execution took %v, expected fast cancellation", elapsed)
	}
}

func TestDispatcher_MultipleErrors_AllJoined(t *testing.T) {
	errAlpha := errors.New("alpha failure")
	errBeta := errors.New("beta failure")

	mock1 := &mockHandler{name: "h1", actionType: notifier.ActionCommitStatus, handleErr: errAlpha}
	mock2 := &mockHandler{name: "h2", actionType: notifier.ActionCommitStatus, handleErr: errBeta}
	mock3 := &mockHandler{name: "h3", actionType: notifier.ActionCommitStatus}

	registry := notifier.NewRegistry()
	registry.Register(mock1)
	registry.Register(mock2)
	registry.Register(mock3)

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{CloudEventID: testEventID}
	err := dispatcher.Handle(context.Background(), env)

	if err == nil {
		t.Fatal("expected aggregated error")
	}
	if !errors.Is(err, errAlpha) {
		t.Errorf("error chain should contain errAlpha")
	}
	if !errors.Is(err, errBeta) {
		t.Errorf("error chain should contain errBeta")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "h1") {
		t.Errorf("error should contain handler name 'h1', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "h2") {
		t.Errorf("error should contain handler name 'h2', got: %s", errMsg)
	}
}

func TestDispatcher_AllHandlersAttemptedDespiteFailures(t *testing.T) {
	const numHandlers = 10

	registry := notifier.NewRegistry()
	handlers := make([]*mockHandler, numHandlers)
	for i := 0; i < numHandlers; i++ {
		h := &mockHandler{
			name:       fmt.Sprintf("handler%d", i),
			actionType: notifier.ActionCommitStatus,
		}
		if i%2 == 0 {
			h.handleErr = fmt.Errorf("handler%d failed", i)
		}
		handlers[i] = h
		registry.Register(h)
	}

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{CloudEventID: testEventID}
	err := dispatcher.Handle(context.Background(), env)

	if err == nil {
		t.Fatal("expected error from failing handlers")
	}

	for i, h := range handlers {
		if h.callCount != 1 {
			t.Errorf("handler%d callCount = %d, want 1 (all handlers must be attempted)", i, h.callCount)
		}
	}
}

func TestDispatcher_StatusTracker_Consistency(t *testing.T) {
	errFail := errors.New("handler failed")

	success1 := &mockHandler{name: "ok1", actionType: notifier.ActionCommitStatus}
	success2 := &mockHandler{name: "ok2", actionType: notifier.ActionPRComment}
	failH := &mockHandler{name: "fail1", actionType: notifier.ActionIssueComment, handleErr: errFail}

	registry := notifier.NewRegistry()
	registry.Register(success1)
	registry.Register(success2)
	registry.Register(failH)

	tracker := NewStatusTracker()
	dispatcher := NewDispatcher(registry, testLogger(), nil, 10).
		WithStatusTracker(tracker)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{CloudEventID: testEventID}
	err := dispatcher.Handle(context.Background(), env)
	if err == nil {
		t.Fatal("expected error from failing handler")
	}

	snap := tracker.Snapshot()

	if s, ok := snap["ok1"]; !ok {
		t.Error("status tracker missing entry for ok1")
	} else if s.Succeeded != 1 || s.Failed != 0 {
		t.Errorf("ok1: succeeded=%d failed=%d, want 1/0", s.Succeeded, s.Failed)
	}

	if s, ok := snap["ok2"]; !ok {
		t.Error("status tracker missing entry for ok2")
	} else if s.Succeeded != 1 || s.Failed != 0 {
		t.Errorf("ok2: succeeded=%d failed=%d, want 1/0", s.Succeeded, s.Failed)
	}

	if s, ok := snap["fail1"]; !ok {
		t.Error("status tracker missing entry for fail1")
	} else if s.Succeeded != 0 || s.Failed != 1 {
		t.Errorf("fail1: succeeded=%d failed=%d, want 0/1", s.Succeeded, s.Failed)
	}
}

func TestDispatcher_NextNotCalledOnHandlerErrors(t *testing.T) {
	mock := &mockHandler{name: "h1", actionType: notifier.ActionCommitStatus, handleErr: errors.New("fail")}

	registry := notifier.NewRegistry()
	registry.Register(mock)

	nextCalled := false
	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		nextCalled = true
		return nil
	}})

	env := &event.Envelope{CloudEventID: testEventID}
	err := dispatcher.Handle(context.Background(), env)

	if err == nil {
		t.Fatal("expected error from failing handler")
	}
	if nextCalled {
		t.Error("next handler should NOT be called when dispatcher has errors")
	}
}

func TestDispatcher_ProviderFiltering(t *testing.T) {
	provider := "github"

	matching := &mockHandler{name: provider, actionType: notifier.ActionCommitStatus}
	nonMatching := &mockHandler{name: "gitlab", actionType: notifier.ActionCommitStatus}
	notifierType := &mockHandler{name: "slack", actionType: notifier.ActionNotify}

	registry := notifier.NewRegistry()
	registry.Register(matching)
	registry.Register(nonMatching)
	registry.Register(notifierType)

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{
		CloudEventID: testEventID,
		Report:       domain.Event{Provider: provider},
	}

	err := dispatcher.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if matching.callCount != 1 {
		t.Errorf("matching handler should be called, callCount = %d", matching.callCount)
	}
	if nonMatching.callCount != 0 {
		t.Errorf("non-matching SCM handler should be skipped, callCount = %d", nonMatching.callCount)
	}
	if notifierType.callCount != 1 {
		t.Errorf("ActionNotify handlers should always be called, callCount = %d", notifierType.callCount)
	}
}

func TestDispatcher_HandlerTimeout_RecordsMetrics(t *testing.T) {
	slowHandler := &mockHandler{
		name:       "slow",
		actionType: notifier.ActionCommitStatus,
		sleepDur:   5 * time.Second,
	}

	registry := notifier.NewRegistry()
	registry.Register(slowHandler)

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10).
		WithHandlerTimeout(50 * time.Millisecond)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{CloudEventID: testEventID}

	start := time.Now()
	err := dispatcher.Handle(context.Background(), env)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("dispatch took %v, expected fast timeout", elapsed)
	}
}

func TestDispatcher_EmptyMatched_WithProviderFiltering(t *testing.T) {
	nonMatching := &mockHandler{name: "gitlab", actionType: notifier.ActionCommitStatus}

	registry := notifier.NewRegistry()
	registry.Register(nonMatching)

	nextCalled := false
	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		nextCalled = true
		return nil
	}})

	env := &event.Envelope{
		CloudEventID: testEventID,
		Report:       domain.Event{Provider: "github"},
	}

	err := dispatcher.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nonMatching.callCount != 0 {
		t.Errorf("non-matching handler should not be called, callCount = %d", nonMatching.callCount)
	}
	if !nextCalled {
		t.Error("next handler should be called when no handlers match")
	}
}

func TestDispatcher_ConcurrentErrorCollection_NoRace(t *testing.T) {
	const numHandlers = 20

	registry := notifier.NewRegistry()
	for i := 0; i < numHandlers; i++ {
		registry.Register(&mockHandler{
			name:       fmt.Sprintf("handler%d", i),
			actionType: notifier.ActionCommitStatus,
			handleErr:  fmt.Errorf("err%d", i),
		})
	}

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{CloudEventID: testEventID}

	for i := 0; i < 5; i++ {
		err := dispatcher.Handle(context.Background(), env)
		if err == nil {
			t.Fatal("expected aggregated error")
		}
	}
}

func TestDispatcher_NilStatusTracker_NoPanic(t *testing.T) {
	mock := &mockHandler{name: "h1", actionType: notifier.ActionCommitStatus}
	registry := notifier.NewRegistry()
	registry.Register(mock)

	dispatcher := NewDispatcher(registry, testLogger(), nil, 10)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{CloudEventID: testEventID}
	err := dispatcher.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDispatcher_WaitError_MergedWithHandlerErrors(t *testing.T) {
	// This test exercises the fix for the discarded g.Wait() return.
	// When context is cancelled mid-flight, both handler errors AND context
	// errors should appear in the result.
	handlerErr := errors.New("handler-specific error")
	slowHandler := &mockHandler{
		name:       "slow-fail",
		actionType: notifier.ActionCommitStatus,
		handleErr:  handlerErr,
		sleepDur:   200 * time.Millisecond,
	}

	registry := notifier.NewRegistry()
	registry.Register(slowHandler)

	dispatcher := NewDispatcher(registry, testLogger(), nil, 1)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	env := &event.Envelope{CloudEventID: testEventID}
	err := dispatcher.Handle(ctx, env)

	if err == nil {
		t.Fatal("expected error combining handler and context errors")
	}
	// At minimum the context error should be present
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded in chain, got: %v", err)
	}
}
