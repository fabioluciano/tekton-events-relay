package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	buildPipeline  = "build-pipeline"
	testPipeline   = "test-pipeline"
	deployPipeline = "deploy-pipeline"
	unitTest       = "unit-test"
)

func TestWrapWithFilter_NilConfig(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	// Nil filterCfg should return handler unchanged
	handler := WrapWithFilter(inner, nil)

	if handler != inner {
		t.Error("expected original handler to be returned when filterCfg is nil")
	}
}

func TestWrapWithFilter_ValidConfig(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline},
		},
	}

	// Valid filterCfg should wrap handler
	handler := WrapWithFilter(inner, cfg)

	if handler == nil {
		t.Fatal("expected wrapped handler, got nil")
	}

	// Verify handler is wrapped (should be FilteredHandler)
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

func TestWrapWithFilter_EmptyConfig(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	// Empty config (no filters) should still wrap and pass all events
	cfg := &config.ActionFilterConfig{}
	handler := WrapWithFilter(inner, cfg)

	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: "any-pipeline",
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called with empty config")
	}
}

func TestWrapWithFilter_AppliesFiltersCorrectly(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline, testPipeline},
		},
		Tasks: config.FilterList{
			Deny: []string{"skip-task"},
		},
	}

	handler := WrapWithFilter(inner, cfg)

	tests := []struct {
		name       string
		event      domain.Event
		wantCalled bool
	}{
		{
			name: "allowed pipeline - should call",
			event: domain.Event{
				Resource:     domain.ResourcePipelineRun,
				PipelineName: buildPipeline,
			},
			wantCalled: true,
		},
		{
			name: "non-allowed pipeline - should skip",
			event: domain.Event{
				Resource:     domain.ResourcePipelineRun,
				PipelineName: deployPipeline,
			},
			wantCalled: false,
		},
		{
			name: "non-denied task - should call",
			event: domain.Event{
				Resource: domain.ResourceTaskRun,
				TaskName: unitTest,
			},
			wantCalled: true,
		},
		{
			name: "denied task - should skip",
			event: domain.Event{
				Resource: domain.ResourceTaskRun,
				TaskName: "skip-task",
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

func TestWrapWithFilter_AllResourceTypes(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	cfg := &config.ActionFilterConfig{
		Tasks: config.FilterList{
			Allow: []string{unitTest},
		},
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline},
		},
		CustomRuns: config.FilterList{
			Deny: []string{"skip-custom"},
		},
		EventListeners: config.FilterList{
			Allow: []string{"github-listener"},
		},
	}

	handler := WrapWithFilter(inner, cfg)

	tests := []struct {
		name       string
		event      domain.Event
		wantCalled bool
	}{
		{
			name: "allowed task",
			event: domain.Event{
				Resource: domain.ResourceTaskRun,
				TaskName: unitTest,
			},
			wantCalled: true,
		},
		{
			name: "non-allowed task",
			event: domain.Event{
				Resource: domain.ResourceTaskRun,
				TaskName: "integration-test",
			},
			wantCalled: false,
		},
		{
			name: "allowed pipeline",
			event: domain.Event{
				Resource:     domain.ResourcePipelineRun,
				PipelineName: buildPipeline,
			},
			wantCalled: true,
		},
		{
			name: "non-allowed pipeline",
			event: domain.Event{
				Resource:     domain.ResourcePipelineRun,
				PipelineName: deployPipeline,
			},
			wantCalled: false,
		},
		{
			name: "non-denied custom run",
			event: domain.Event{
				Resource: domain.ResourceCustomRun,
				TaskName: "approval-task",
			},
			wantCalled: true,
		},
		{
			name: "denied custom run",
			event: domain.Event{
				Resource: domain.ResourceCustomRun,
				TaskName: "skip-custom",
			},
			wantCalled: false,
		},
		{
			name: "allowed event listener",
			event: domain.Event{
				Resource:          domain.ResourceEventListener,
				EventListenerName: "github-listener",
			},
			wantCalled: true,
		},
		{
			name: "non-allowed event listener",
			event: domain.Event{
				Resource:          domain.ResourceEventListener,
				EventListenerName: "bitbucket-listener",
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

func TestWrapWithFilter_CaseInsensitive(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{"Build-Pipeline", "TEST-pipeline"},
		},
	}

	handler := WrapWithFilter(inner, cfg)

	tests := []struct {
		name         string
		pipelineName string
		wantCalled   bool
	}{
		{
			name:         "lowercase matches mixed case allow",
			pipelineName: buildPipeline,
			wantCalled:   true,
		},
		{
			name:         "uppercase matches mixed case allow",
			pipelineName: "BUILD-PIPELINE",
			wantCalled:   true,
		},
		{
			name:         "mixed case matches uppercase allow",
			pipelineName: "Test-Pipeline",
			wantCalled:   true,
		},
		{
			name:         "non-matching case-insensitive",
			pipelineName: deployPipeline,
			wantCalled:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner.called = false

			event := domain.Event{
				Resource:     domain.ResourcePipelineRun,
				PipelineName: tt.pipelineName,
			}

			err := handler.Handle(context.Background(), event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if inner.called != tt.wantCalled {
				t.Errorf("expected called=%v, got %v", tt.wantCalled, inner.called)
			}
		})
	}
}

func TestWrapWithFilter_DenyWinsOverAllow(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline, testPipeline},
			Deny:  []string{buildPipeline},
		},
	}

	handler := WrapWithFilter(inner, cfg)

	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: buildPipeline,
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inner.called {
		t.Error("expected inner handler NOT to be called (deny wins over allow)")
	}
}

func TestWrapWithFilter_EmptyNamePassesThrough(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline},
		},
	}

	handler := WrapWithFilter(inner, cfg)

	// Event with empty pipeline name should pass through (cannot filter)
	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: "",
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called for empty name (cannot filter)")
	}
}

func TestWrapWithFilter_UnknownResourcePassesThrough(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline},
		},
	}

	handler := WrapWithFilter(inner, cfg)

	// Unknown resource type should pass through
	event := domain.Event{
		Resource: "unknown-resource-type",
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called for unknown resource type")
	}
}

func TestWrapWithFilter_ErrorPropagation(t *testing.T) {
	expectedErr := context.Canceled
	inner := &mockActionHandler{
		name: testHandler,
		typ:  notifier.ActionCommitStatus,
		err:  expectedErr,
	}

	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline},
		},
	}

	handler := WrapWithFilter(inner, cfg)

	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: buildPipeline,
	}

	err := handler.Handle(context.Background(), event)
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected inner handler error to propagate, got %v", err)
	}
}

func TestWrapWithFilter_MultipleWrappers(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	// First filter: allow only build-pipeline
	cfg1 := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline, testPipeline},
		},
	}
	handler1 := WrapWithFilter(inner, cfg1)

	// Second filter: deny test-pipeline (should be redundant with first filter for most cases)
	cfg2 := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Deny: []string{testPipeline},
		},
	}
	handler2 := WrapWithFilter(handler1, cfg2)

	tests := []struct {
		name         string
		pipelineName string
		wantCalled   bool
	}{
		{
			name:         "build-pipeline allowed by both filters",
			pipelineName: buildPipeline,
			wantCalled:   true,
		},
		{
			name:         "test-pipeline allowed by first but denied by second",
			pipelineName: testPipeline,
			wantCalled:   false,
		},
		{
			name:         "deploy-pipeline denied by first filter",
			pipelineName: deployPipeline,
			wantCalled:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner.called = false

			event := domain.Event{
				Resource:     domain.ResourcePipelineRun,
				PipelineName: tt.pipelineName,
			}

			err := handler2.Handle(context.Background(), event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if inner.called != tt.wantCalled {
				t.Errorf("expected called=%v, got %v", tt.wantCalled, inner.called)
			}
		})
	}
}

func TestWrapWithFilter_OnlyDenyList(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}

	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Deny: []string{deployPipeline},
		},
	}

	handler := WrapWithFilter(inner, cfg)

	tests := []struct {
		name         string
		pipelineName string
		wantCalled   bool
	}{
		{
			name:         "non-denied pipeline passes",
			pipelineName: buildPipeline,
			wantCalled:   true,
		},
		{
			name:         "denied pipeline drops",
			pipelineName: deployPipeline,
			wantCalled:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner.called = false

			event := domain.Event{
				Resource:     domain.ResourcePipelineRun,
				PipelineName: tt.pipelineName,
			}

			err := handler.Handle(context.Background(), event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if inner.called != tt.wantCalled {
				t.Errorf("expected called=%v, got %v", tt.wantCalled, inner.called)
			}
		})
	}
}
