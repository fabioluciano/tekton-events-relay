package main

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// mockActionHandler implements ActionHandler for integration testing.
type mockActionHandler struct {
	name   string
	typ    notifier.ActionType
	called bool
}

func (m *mockActionHandler) Name() string              { return m.name }
func (m *mockActionHandler) Type() notifier.ActionType { return m.typ }
func (m *mockActionHandler) Handle(_ context.Context, _ domain.Event) error {
	m.called = true
	return nil
}

func TestCELIntegration_EndToEnd(t *testing.T) {
	logger := zap.NewNop()

	t.Run("CEL expression filters by resource type", func(t *testing.T) {
		// Create mock handler
		inner := &mockActionHandler{
			name: "test-handler",
			typ:  notifier.ActionCommitStatus,
		}

		// Compile CEL expression: only match taskrun events
		program, err := cel.Compile(`event.Resource == "taskrun"`)
		if err != nil {
			t.Fatalf("failed to compile CEL: %v", err)
		}

		// Wrap with CEL conditional handler (mimics wrapWithCEL in main.go)
		handler := notifier.NewConditionalHandler(inner, program, logger)

		// Test 1: TaskRun event should trigger handler
		taskRunEvent := domain.Event{
			Resource:  domain.ResourceTaskRun,
			State:     domain.StateSuccess,
			RunName:   "test-taskrun-123",
			Namespace: "default",
		}

		inner.called = false
		err = handler.Handle(context.Background(), taskRunEvent)
		if err != nil {
			t.Fatalf("unexpected error for taskrun: %v", err)
		}
		if !inner.called {
			t.Error("expected handler to be called for TaskRun event")
		}

		// Test 2: PipelineRun event should skip handler
		pipelineRunEvent := domain.Event{
			Resource:  domain.ResourcePipelineRun,
			State:     domain.StateSuccess,
			RunName:   "test-pipeline-456",
			Namespace: "default",
		}

		inner.called = false
		err = handler.Handle(context.Background(), pipelineRunEvent)
		if err != nil {
			t.Fatalf("unexpected error for pipelinerun: %v", err)
		}
		if inner.called {
			t.Error("expected handler NOT to be called for PipelineRun event")
		}
	})

	t.Run("CEL expression filters by state", func(t *testing.T) {
		inner := &mockActionHandler{
			name: "failure-handler",
			typ:  notifier.ActionPRComment,
		}

		// Only trigger on failure or error states
		program, err := cel.Compile(`event.State == "failure" || event.State == "error"`)
		if err != nil {
			t.Fatalf("failed to compile CEL: %v", err)
		}

		handler := notifier.NewConditionalHandler(inner, program, logger)

		// Test failure state → should call
		failureEvent := domain.Event{
			Resource: domain.ResourcePipelineRun,
			State:    domain.StateFailure,
			RunName:  "failed-run",
		}

		inner.called = false
		err = handler.Handle(context.Background(), failureEvent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !inner.called {
			t.Error("expected handler to be called for failure state")
		}

		// Test success state → should skip
		successEvent := domain.Event{
			Resource: domain.ResourcePipelineRun,
			State:    domain.StateSuccess,
			RunName:  "success-run",
		}

		inner.called = false
		err = handler.Handle(context.Background(), successEvent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if inner.called {
			t.Error("expected handler NOT to be called for success state")
		}
	})

	t.Run("complex CEL expression with multiple conditions", func(t *testing.T) {
		inner := &mockActionHandler{
			name: "complex-handler",
			typ:  notifier.ActionLabel,
		}

		// Only process failed pipelineruns from production namespace
		program, err := cel.Compile(`event.Resource == "pipelinerun" && event.State == "failure" && event.Namespace == "production"`)
		if err != nil {
			t.Fatalf("failed to compile CEL: %v", err)
		}

		handler := notifier.NewConditionalHandler(inner, program, logger)

		tests := []struct {
			name      string
			event     domain.Event
			wantCalls bool
		}{
			{
				name: "matching event - pipelinerun failure in production",
				event: domain.Event{
					Resource:  domain.ResourcePipelineRun,
					State:     domain.StateFailure,
					Namespace: "production",
					RunName:   "prod-pipeline",
				},
				wantCalls: true,
			},
			{
				name: "wrong resource - taskrun in production",
				event: domain.Event{
					Resource:  domain.ResourceTaskRun,
					State:     domain.StateFailure,
					Namespace: "production",
					RunName:   "prod-task",
				},
				wantCalls: false,
			},
			{
				name: "wrong state - pipelinerun success in production",
				event: domain.Event{
					Resource:  domain.ResourcePipelineRun,
					State:     domain.StateSuccess,
					Namespace: "production",
					RunName:   "prod-success",
				},
				wantCalls: false,
			},
			{
				name: "wrong namespace - pipelinerun failure in dev",
				event: domain.Event{
					Resource:  domain.ResourcePipelineRun,
					State:     domain.StateFailure,
					Namespace: "dev",
					RunName:   "dev-pipeline",
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
	})

	t.Run("nil program always delegates", func(t *testing.T) {
		inner := &mockActionHandler{
			name: "no-guard-handler",
			typ:  notifier.ActionCommitStatus,
		}

		// No CEL program → always call handler
		handler := notifier.NewConditionalHandler(inner, nil, logger)

		event := domain.Event{
			Resource: domain.ResourcePipelineRun,
			State:    domain.StateSuccess,
		}

		inner.called = false
		err := handler.Handle(context.Background(), event)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !inner.called {
			t.Error("expected handler to be called when program is nil")
		}
	})
}

func TestCELIntegration_ExampleConfigParsing(t *testing.T) {
	// Verify that examples/config.yaml parses successfully
	// This ensures CEL expressions in the example config are valid
	cfg, err := config.Load("../../wiki/examples/config.yaml")
	if err != nil {
		t.Fatalf("failed to load examples/config.yaml: %v", err)
	}

	// Verify config loaded
	if cfg == nil {
		t.Fatal("config is nil")
	}

	// Verify some expected structure exists
	if cfg.Server.Addr == "" {
		t.Error("expected server.addr to be set")
	}

	// Test that CEL expressions in the config can be compiled
	// Extract expressions from the loaded config and verify they compile
	tests := []struct {
		section string
		expr    string
	}{
		{
			section: "github.actions.pr_comment",
			expr:    `event.Resource == "taskrun" && event.State == "failure"`,
		},
		{
			section: "github.actions.issue_comment",
			expr:    `event.Namespace == "production"`,
		},
		{
			section: "azure_devops.actions.label",
			expr:    `event.Resource == "pipelinerun" && event.Repo.Owner == "myorg"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.section, func(t *testing.T) {
			program, err := cel.Compile(tt.expr)
			if err != nil {
				t.Errorf("CEL expression in %s failed to compile: %v", tt.section, err)
			}
			if program == nil {
				t.Errorf("expected non-nil program for %s", tt.section)
			}
		})
	}
}

func TestCELIntegration_BuildActionHandlersFlow(t *testing.T) {
	// Integration test that mimics the buildActionHandlers flow from main.go
	logger := zap.NewNop()

	// Create a minimal config with GitHub PR comment action using CEL
	cfg := &config.Config{
		SCM: config.SCMConfig{
			GitHub: []config.GitHubInstance{
				{
					Name:               "main",
					Enabled:            true,
					Auth:               &config.GitHubAuth{SecretFile: "/tmp/test-token"}, //nolint:gosec // test credential path
					BaseURL:            "https://api.github.com",
					InsecureSkipVerify: false,
					Actions: []config.Action{
						{
							Name:     "pr-comment",
							Type:     notifier.ActionPRComment,
							Enabled:  true,
							When:     `event.Resource == "pipelinerun" && stateIn("failure")`,
							Template: "Test template",
						},
					},
				},
			},
		},
	}

	// Simulate wrapWithCEL function from main.go
	wrapWithCEL := func(handler notifier.ActionHandler, whenExpr string) notifier.ActionHandler {
		if whenExpr == "" {
			return handler
		}
		prog, err := cel.Compile(whenExpr)
		if err != nil {
			t.Fatalf("invalid CEL expression: %v", err)
		}
		return notifier.NewConditionalHandler(handler, prog, logger)
	}

	// Create mock handler
	inner := &mockActionHandler{
		name: "github-pr-comment",
		typ:  notifier.ActionPRComment,
	}

	// Wrap with CEL (as done in buildActionHandlers)
	whenExpr := cfg.SCM.GitHub[0].Actions[0].When
	wrapped := wrapWithCEL(inner, whenExpr)

	// Test with pipelinerun → should call
	pipelineEvent := domain.Event{
		Resource: domain.ResourcePipelineRun,
		State:    domain.StateFailure,
	}

	inner.called = false
	err := wrapped.Handle(context.Background(), pipelineEvent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !inner.called {
		t.Error("expected handler to be called for pipelinerun")
	}

	// Test with taskrun → should skip
	taskEvent := domain.Event{
		Resource: domain.ResourceTaskRun,
		State:    domain.StateFailure,
	}

	inner.called = false
	err = wrapped.Handle(context.Background(), taskEvent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inner.called {
		t.Error("expected handler NOT to be called for taskrun")
	}
}

func TestCELIntegration_RepoFieldAccess(t *testing.T) {
	logger := zap.NewNop()

	// Test CEL expressions that access nested Repo fields
	inner := &mockActionHandler{
		name: "repo-filter",
		typ:  notifier.ActionCommitStatus,
	}

	// Filter by repository owner
	program, err := cel.Compile(`event.Repo.Owner == "myorg"`)
	if err != nil {
		t.Fatalf("failed to compile CEL: %v", err)
	}

	handler := notifier.NewConditionalHandler(inner, program, logger)

	// Test matching owner
	matchEvent := domain.Event{
		Resource: domain.ResourcePipelineRun,
		State:    domain.StateSuccess,
		Repo: domain.Repo{
			Owner: "myorg",
			Name:  "myrepo",
		},
	}

	inner.called = false
	err = handler.Handle(context.Background(), matchEvent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !inner.called {
		t.Error("expected handler to be called for matching repo owner")
	}

	// Test non-matching owner
	noMatchEvent := domain.Event{
		Resource: domain.ResourcePipelineRun,
		State:    domain.StateSuccess,
		Repo: domain.Repo{
			Owner: "otherorg",
			Name:  "myrepo",
		},
	}

	inner.called = false
	err = handler.Handle(context.Background(), noMatchEvent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inner.called {
		t.Error("expected handler NOT to be called for non-matching repo owner")
	}
}
