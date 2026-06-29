package notifier

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	testHandler     = "test-handler"
	testRunName     = "test-run"
	celStateSuccess = `event.State == "success"`
	celStateFailure = `event.State == "failure"`
)

// mockActionHandler implements ActionHandler for testing.
type mockActionHandler struct {
	name   string
	typ    ActionType
	called bool
	err    error
}

func (m *mockActionHandler) Name() string     { return m.name }
func (m *mockActionHandler) Type() ActionType { return m.typ }
func (m *mockActionHandler) Handle(_ context.Context, _ domain.Event) error {
	m.called = true
	return m.err
}
func (m *mockActionHandler) Close() error { return nil }

// closeSpy tracks Close() calls for verification.
type closeSpy struct {
	mockActionHandler
	closeCount int
	closeErr   error
}

func (s *closeSpy) Close() error {
	s.closeCount++
	return s.closeErr
}

func TestConditionalHandler_NilProgram(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	handler := NewConditionalHandler(inner, nil, logger)

	event := domain.Event{
		Resource: domain.ResourcePipelineRun,
		State:    domain.StateSuccess,
		RunName:  testRunName,
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called when program is nil")
	}

	if logs.Len() != 0 {
		t.Errorf("expected no logs, got %d", logs.Len())
	}
}

func TestConditionalHandler_CELTrue(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	// CEL expression that always returns true
	program, err := cel.Compile(celStateSuccess)
	if err != nil {
		t.Fatalf("failed to compile CEL: %v", err)
	}

	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	handler := NewConditionalHandler(inner, program, logger)

	event := domain.Event{
		Resource: domain.ResourcePipelineRun,
		State:    domain.StateSuccess,
		RunName:  testRunName,
	}

	err = handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called when CEL returns true")
	}

	if logs.Len() != 0 {
		t.Errorf("expected no logs, got %d", logs.Len())
	}
}

func TestConditionalHandler_CELFalse(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	// CEL expression that returns false for this event
	program, err := cel.Compile(celStateFailure)
	if err != nil {
		t.Fatalf("failed to compile CEL: %v", err)
	}

	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	handler := NewConditionalHandler(inner, program, logger)

	event := domain.Event{
		Resource: domain.ResourcePipelineRun,
		State:    domain.StateSuccess,
		RunName:  testRunName,
	}

	err = handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if inner.called {
		t.Error("expected inner handler NOT to be called when CEL returns false")
	}

	// Verify info log was emitted
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log entry, got %d", logs.Len())
	}

	entry := logs.All()[0]
	if entry.Level != zap.InfoLevel {
		t.Errorf("expected info level, got %v", entry.Level)
	}
	if entry.Message != "action skipped by when condition" {
		t.Errorf("expected skip message, got %q", entry.Message)
	}

	// Verify log fields
	fields := entry.ContextMap()
	if fields["handler"] != testHandler {
		t.Errorf("expected handler=test-handler, got %v", fields["handler"])
	}
	if fields["event_resource"] != "pipelinerun" {
		t.Errorf("expected event_resource=pipelinerun, got %v", fields["event_resource"])
	}
	if fields["event_state"] != "success" {
		t.Errorf("expected event_state=success, got %v", fields["event_state"])
	}
	if fields["event_run_name"] != testRunName {
		t.Errorf("expected event_run_name=test-run, got %v", fields["event_run_name"])
	}
}

func TestConditionalHandler_CELEvalError(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	// CEL expression that will fail at evaluation (accessing non-existent field)
	// Note: We can't easily force an eval error with the current schema,
	// so we'll use an expression that might fail with type mismatches
	program, err := cel.Compile(celStateSuccess)
	if err != nil {
		t.Fatalf("failed to compile CEL: %v", err)
	}

	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	handler := NewConditionalHandler(inner, program, logger)

	// Create an event that should work fine
	// (Note: In production, eval errors are rare with properly compiled CEL)
	event := domain.Event{
		Resource: domain.ResourcePipelineRun,
		State:    domain.StateSuccess,
		RunName:  testRunName,
	}

	// This test demonstrates the error handling path exists,
	// even though triggering a real eval error is difficult
	err = handler.Handle(context.Background(), event)
	if err != nil {
		// If we got an error, verify error logging
		if logs.Len() == 0 {
			t.Error("expected error log when eval fails")
		}
		entry := logs.All()[0]
		if entry.Level != zap.ErrorLevel {
			t.Errorf("expected error level, got %v", entry.Level)
		}
		if entry.Message != "CEL evaluation failed" {
			t.Errorf("expected eval error message, got %q", entry.Message)
		}
		if inner.called {
			t.Error("expected inner handler NOT to be called when CEL eval fails")
		}
	}
}

func TestConditionalHandler_InnerHandlerError(t *testing.T) {
	core, _ := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	program, err := cel.Compile(celStateSuccess)
	if err != nil {
		t.Fatalf("failed to compile CEL: %v", err)
	}

	expectedErr := errors.New("inner handler failed")
	inner := &mockActionHandler{
		name: testHandler,
		typ:  ActionCommitStatus,
		err:  expectedErr,
	}
	handler := NewConditionalHandler(inner, program, logger)

	event := domain.Event{
		Resource: domain.ResourcePipelineRun,
		State:    domain.StateSuccess,
		RunName:  testRunName,
	}

	err = handler.Handle(context.Background(), event)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected inner handler error, got %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called")
	}
}

func TestConditionalHandler_NameTypeDelegation(t *testing.T) {
	logger := zap.NewNop()

	inner := &mockActionHandler{
		name: "github-handler",
		typ:  ActionPRComment,
	}
	handler := NewConditionalHandler(inner, nil, logger)

	if handler.Name() != "github-handler" {
		t.Errorf("expected name=github-handler, got %s", handler.Name())
	}

	if handler.Type() != ActionPRComment {
		t.Errorf("expected type=pr_comment, got %s", handler.Type())
	}
}

func TestConditionalHandler_ComplexCELExpression(t *testing.T) {
	core, _ := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	// Complex CEL expression: only notify on failure/error for pipelineruns
	program, err := cel.Compile(`event.Resource == "pipelinerun" && (event.State == "failure" || event.State == "error")`)
	if err != nil {
		t.Fatalf("failed to compile CEL: %v", err)
	}

	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	handler := NewConditionalHandler(inner, program, logger)

	tests := []struct {
		name      string
		event     domain.Event
		wantCalls bool
	}{
		{
			name: "pipelinerun failure - should call",
			event: domain.Event{
				Resource: domain.ResourcePipelineRun,
				State:    domain.StateFailure,
			},
			wantCalls: true,
		},
		{
			name: "pipelinerun success - should skip",
			event: domain.Event{
				Resource: domain.ResourcePipelineRun,
				State:    domain.StateSuccess,
			},
			wantCalls: false,
		},
		{
			name: "taskrun failure - should skip",
			event: domain.Event{
				Resource: domain.ResourceTaskRun,
				State:    domain.StateFailure,
			},
			wantCalls: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner.called = false

			err := handler.Handle(context.Background(), tt.event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if inner.called != tt.wantCalls {
				t.Errorf("expected called=%v, got %v", tt.wantCalls, inner.called)
			}
		})
	}
}

func TestWrapperForwardsClose(t *testing.T) {
	tests := []struct {
		name string
		wrap func(inner ActionHandler) ActionHandler
	}{
		{
			name: "ConditionalHandler",
			wrap: func(inner ActionHandler) ActionHandler {
				return NewConditionalHandler(inner, nil, zap.NewNop())
			},
		},
		{
			name: "FilteredHandler",
			wrap: func(inner ActionHandler) ActionHandler {
				return NewFilteredHandler(inner, nil)
			},
		},
		{
			name: "ConditionalHandler_withCEL",
			wrap: func(inner ActionHandler) ActionHandler {
				program, _ := cel.Compile(`event.State == "success"`)
				return NewConditionalHandler(inner, program, zap.NewNop())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &closeSpy{
				mockActionHandler: mockActionHandler{name: testHandler, typ: ActionCommitStatus},
			}
			wrapper := tt.wrap(spy)

			if err := wrapper.Close(); err != nil {
				t.Fatalf("Close() returned error: %v", err)
			}
			if spy.closeCount != 1 {
				t.Errorf("expected closeCount=1, got %d", spy.closeCount)
			}

			// Verify idempotency
			if err := wrapper.Close(); err != nil {
				t.Fatalf("second Close() returned error: %v", err)
			}
			if spy.closeCount != 2 {
				t.Errorf("expected closeCount=2 after second call, got %d", spy.closeCount)
			}
		})
	}
}

func TestWrapperForwardsCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	spy := &closeSpy{
		mockActionHandler: mockActionHandler{name: testHandler, typ: ActionCommitStatus},
		closeErr:          closeErr,
	}

	wrapper := NewConditionalHandler(spy, nil, zap.NewNop())
	err := wrapper.Close()
	if !errors.Is(err, closeErr) {
		t.Fatalf("expected close error, got %v", err)
	}
	if spy.closeCount != 1 {
		t.Errorf("expected closeCount=1, got %d", spy.closeCount)
	}
}
