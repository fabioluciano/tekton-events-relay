package middleware

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	testHandler     = "test-handler"
	celStateSuccess = `event.State == "success"`
	testRunName     = "test-run"
)

// mockActionHandler implements ActionHandler for testing.
type mockActionHandler struct {
	name   string
	typ    notifier.ActionType
	called bool
	err    error
}

func (m *mockActionHandler) Name() string              { return m.name }
func (m *mockActionHandler) Type() notifier.ActionType { return m.typ }
func (m *mockActionHandler) Handle(_ context.Context, _ domain.Event) error {
	m.called = true
	return m.err
}
func (m *mockActionHandler) Close() error { return nil }

func TestWrapWithCEL_EmptyExpression(t *testing.T) {
	logger := zap.NewNop()
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	// Empty whenExpr should return handler unchanged
	handler, err := WrapWithCEL(inner, "", logger)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if handler != inner {
		t.Error("expected original handler to be returned when whenExpr is empty")
	}
}

func TestWrapWithCEL_ValidExpression(t *testing.T) {
	logger := zap.NewNop()
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	// Valid CEL expression should compile and wrap
	handler, err := WrapWithCEL(inner, celStateSuccess, logger)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if handler == nil {
		t.Fatal("expected wrapped handler, got nil")
	}

	// Verify handler is wrapped (should be ConditionalHandler)
	if handler == inner {
		t.Error("expected wrapped handler, got original handler")
	}

	// Verify wrapped handler delegates Name() and Type()
	if handler.Name() != testHandler {
		t.Errorf("expected name=test-handler, got %s", handler.Name())
	}
	if handler.Type() != notifier.ActionCommitStatus {
		t.Errorf("expected type=commit_status, got %s", handler.Type())
	}
}

func TestWrapWithCEL_InvalidExpression(t *testing.T) {
	logger := zap.NewNop()
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	tests := []struct {
		name     string
		whenExpr string
	}{
		{
			name:     "syntax error",
			whenExpr: `event.State ==`,
		},
		{
			name:     "non-boolean expression",
			whenExpr: `event.State`,
		},
		{
			name:     "malformed expression",
			whenExpr: `((event.State == "success"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := WrapWithCEL(inner, tt.whenExpr, logger)
			if err == nil {
				t.Fatal("expected error for invalid CEL expression")
			}

			if handler != nil {
				t.Errorf("expected nil handler on error, got %v", handler)
			}
		})
	}
}

func TestWrapWithCEL_WrappedHandlerExecutesCELGuard(t *testing.T) {
	logger := zap.NewNop()
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	// CEL expression that only allows success state
	handler, err := WrapWithCEL(inner, celStateSuccess, logger)
	if err != nil {
		t.Fatalf("failed to compile CEL: %v", err)
	}

	tests := []struct {
		name        string
		event       domain.Event
		wantCalled  bool
		description string
	}{
		{
			name: "success state - should call",
			event: domain.Event{
				Resource: domain.ResourcePipelineRun,
				State:    domain.StateSuccess,
				RunName:  testRunName,
			},
			wantCalled:  true,
			description: "CEL guard should pass for success state",
		},
		{
			name: "failure state - should skip",
			event: domain.Event{
				Resource: domain.ResourcePipelineRun,
				State:    domain.StateFailure,
				RunName:  testRunName,
			},
			wantCalled:  false,
			description: "CEL guard should reject failure state",
		},
		{
			name: "running state - should skip",
			event: domain.Event{
				Resource: domain.ResourceTaskRun,
				State:    domain.StateRunning,
				RunName:  testRunName,
			},
			wantCalled:  false,
			description: "CEL guard should reject running state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner.called = false

			err := handler.Handle(context.Background(), tt.event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if inner.called != tt.wantCalled {
				t.Errorf("%s: expected called=%v, got %v", tt.description, tt.wantCalled, inner.called)
			}
		})
	}
}

func TestWrapWithCEL_ComplexExpression(t *testing.T) {
	logger := zap.NewNop()
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	// Complex CEL: only notify on failure/error for pipelineruns
	handler, err := WrapWithCEL(inner, `event.Resource == "pipelinerun" && (event.State == "failure" || event.State == "error")`, logger)
	if err != nil {
		t.Fatalf("failed to compile CEL: %v", err)
	}

	tests := []struct {
		name       string
		event      domain.Event
		wantCalled bool
	}{
		{
			name: "pipelinerun failure - should call",
			event: domain.Event{
				Resource: domain.ResourcePipelineRun,
				State:    domain.StateFailure,
			},
			wantCalled: true,
		},
		{
			name: "pipelinerun error - should call",
			event: domain.Event{
				Resource: domain.ResourcePipelineRun,
				State:    domain.StateError,
			},
			wantCalled: true,
		},
		{
			name: "pipelinerun success - should skip",
			event: domain.Event{
				Resource: domain.ResourcePipelineRun,
				State:    domain.StateSuccess,
			},
			wantCalled: false,
		},
		{
			name: "taskrun failure - should skip",
			event: domain.Event{
				Resource: domain.ResourceTaskRun,
				State:    domain.StateFailure,
			},
			wantCalled: false,
		},
		{
			name: "customrun error - should skip",
			event: domain.Event{
				Resource: domain.ResourceCustomRun,
				State:    domain.StateError,
			},
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner.called = false

			err := handler.Handle(context.Background(), tt.event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if inner.called != tt.wantCalled {
				t.Errorf("expected called=%v, got %v", tt.wantCalled, inner.called)
			}
		})
	}
}

func TestWrapWithCEL_ErrorPropagation(t *testing.T) {
	logger := zap.NewNop()

	// Test that inner handler errors are propagated
	t.Run("inner handler error propagates", func(t *testing.T) {
		expectedErr := context.Canceled
		inner := &mockActionHandler{
			name: testHandler,
			typ:  notifier.ActionCommitStatus,
			err:  expectedErr,
		}

		handler, err := WrapWithCEL(inner, celStateSuccess, logger)
		if err != nil {
			t.Fatalf("failed to wrap: %v", err)
		}

		event := domain.Event{
			State: domain.StateSuccess,
		}

		err = handler.Handle(context.Background(), event)
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected inner handler error to propagate, got %v", err)
		}
	})
}

func TestWrapWithCEL_PrecompiledProgram(t *testing.T) {
	logger := zap.NewNop()
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	// Compile the program once
	program, err := cel.Compile(celStateSuccess)
	if err != nil {
		t.Fatalf("failed to compile CEL: %v", err)
	}

	// Wrap handler using WrapWithCEL (which compiles internally)
	handler, err := WrapWithCEL(inner, celStateSuccess, logger)
	if err != nil {
		t.Fatalf("failed to wrap: %v", err)
	}

	// Verify compilation is idempotent
	_, err = cel.Compile(celStateSuccess)
	if err != nil {
		t.Fatalf("repeated compilation should work: %v", err)
	}

	// Verify wrapped handler works correctly
	event := domain.Event{State: domain.StateSuccess}
	err = handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called")
	}

	// Verify program can be used directly too
	result, err := program.Eval(event)
	if err != nil {
		t.Fatalf("program eval failed: %v", err)
	}
	if !result {
		t.Error("expected program to return true for success state")
	}
}

func TestWrapWithCEL_MultipleWrappers(t *testing.T) {
	logger := zap.NewNop()
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	// First wrap: only pipelineruns
	handler1, err := WrapWithCEL(inner, `event.Resource == "pipelinerun"`, logger)
	if err != nil {
		t.Fatalf("failed first wrap: %v", err)
	}

	// Second wrap: only success state
	handler2, err := WrapWithCEL(handler1, celStateSuccess, logger)
	if err != nil {
		t.Fatalf("failed second wrap: %v", err)
	}

	tests := []struct {
		name       string
		event      domain.Event
		wantCalled bool
	}{
		{
			name: "pipelinerun success - should call",
			event: domain.Event{
				Resource: domain.ResourcePipelineRun,
				State:    domain.StateSuccess,
			},
			wantCalled: true,
		},
		{
			name: "pipelinerun failure - should skip",
			event: domain.Event{
				Resource: domain.ResourcePipelineRun,
				State:    domain.StateFailure,
			},
			wantCalled: false,
		},
		{
			name: "taskrun success - should skip",
			event: domain.Event{
				Resource: domain.ResourceTaskRun,
				State:    domain.StateSuccess,
			},
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner.called = false

			err := handler2.Handle(context.Background(), tt.event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if inner.called != tt.wantCalled {
				t.Errorf("expected called=%v, got %v", tt.wantCalled, inner.called)
			}
		})
	}
}
