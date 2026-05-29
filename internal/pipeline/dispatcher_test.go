package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

	dispatcher := NewDispatcher(registry, testLogger())
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

	dispatcher := NewDispatcher(registry, testLogger())
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
	dispatcher := NewDispatcher(registry, testLogger())
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

	dispatcher := NewDispatcher(registry, testLogger())
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

	dispatcher := NewDispatcher(registry, testLogger())
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

	dispatcher := NewDispatcher(registry, testLogger())
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

	dispatcher := NewDispatcher(registry, testLogger())
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

	dispatcher := NewDispatcher(registry, logger)
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
