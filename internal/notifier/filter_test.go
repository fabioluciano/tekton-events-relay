package notifier

import (
	"context"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	buildPipeline  = "build-pipeline"
	testPipeline   = "test-pipeline"
	deployPipeline = "deploy-pipeline"
	unitTest       = "unit-test"
)

func TestFilteredHandler_NilConfig(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	handler := NewFilteredHandler(inner, nil)

	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: buildPipeline,
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called when config is nil")
	}
}

func TestFilteredHandler_EmptyFilter(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		// All filter lists empty
	}
	handler := NewFilteredHandler(inner, cfg)

	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: buildPipeline,
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called with empty filter")
	}
}

func TestFilteredHandler_AllowOnly_Pass(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline, testPipeline},
		},
	}
	handler := NewFilteredHandler(inner, cfg)

	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: buildPipeline,
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called for allowed pipeline")
	}
}

func TestFilteredHandler_AllowOnly_Drop(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline, testPipeline},
		},
	}
	handler := NewFilteredHandler(inner, cfg)

	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: deployPipeline,
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if inner.called {
		t.Error("expected inner handler NOT to be called for non-allowed pipeline")
	}
}

func TestFilteredHandler_DenyOnly_Pass(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Deny: []string{deployPipeline},
		},
	}
	handler := NewFilteredHandler(inner, cfg)

	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: buildPipeline,
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called for non-denied pipeline")
	}
}

func TestFilteredHandler_DenyOnly_Drop(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Deny: []string{deployPipeline, testPipeline},
		},
	}
	handler := NewFilteredHandler(inner, cfg)

	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: deployPipeline,
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if inner.called {
		t.Error("expected inner handler NOT to be called for denied pipeline")
	}
}

func TestFilteredHandler_BothAllowAndDeny_DenyWins(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline, testPipeline},
			Deny:  []string{buildPipeline},
		},
	}
	handler := NewFilteredHandler(inner, cfg)

	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: buildPipeline,
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if inner.called {
		t.Error("expected inner handler NOT to be called (deny wins over allow)")
	}
}

func TestFilteredHandler_CaseInsensitive(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{"Build-Pipeline", "TEST-pipeline"},
		},
	}
	handler := NewFilteredHandler(inner, cfg)

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
				t.Fatalf("expected nil error, got %v", err)
			}

			if inner.called != tt.wantCalled {
				t.Errorf("expected called=%v, got %v", tt.wantCalled, inner.called)
			}
		})
	}
}

//nolint:dupl // Test pattern repetition is acceptable
func TestFilteredHandler_TaskRun(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		Tasks: config.FilterList{
			Allow: []string{unitTest, "integration-test"},
		},
	}
	handler := NewFilteredHandler(inner, cfg)

	tests := []struct {
		name       string
		taskName   string
		wantCalled bool
	}{
		{
			name:       "allowed task passes",
			taskName:   unitTest,
			wantCalled: true,
		},
		{
			name:       "non-allowed task drops",
			taskName:   "build-task",
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner.called = false

			event := domain.Event{
				Resource: domain.ResourceTaskRun,
				TaskName: tt.taskName,
			}

			err := handler.Handle(context.Background(), event)
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}

			if inner.called != tt.wantCalled {
				t.Errorf("expected called=%v, got %v", tt.wantCalled, inner.called)
			}
		})
	}
}

func TestFilteredHandler_CustomRun(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		CustomRuns: config.FilterList{
			Deny: []string{"skip-custom"},
		},
	}
	handler := NewFilteredHandler(inner, cfg)

	tests := []struct {
		name       string
		taskName   string
		wantCalled bool
	}{
		{
			name:       "non-denied custom run passes",
			taskName:   "approval-task",
			wantCalled: true,
		},
		{
			name:       "denied custom run drops",
			taskName:   "skip-custom",
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner.called = false

			event := domain.Event{
				Resource: domain.ResourceCustomRun,
				TaskName: tt.taskName, // CustomRun uses TaskName field
			}

			err := handler.Handle(context.Background(), event)
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}

			if inner.called != tt.wantCalled {
				t.Errorf("expected called=%v, got %v", tt.wantCalled, inner.called)
			}
		})
	}
}

//nolint:dupl // Test pattern repetition is acceptable
func TestFilteredHandler_EventListener(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		EventListeners: config.FilterList{
			Allow: []string{"github-listener", "gitlab-listener"},
		},
	}
	handler := NewFilteredHandler(inner, cfg)

	tests := []struct {
		name              string
		eventListenerName string
		wantCalled        bool
	}{
		{
			name:              "allowed event listener passes",
			eventListenerName: "github-listener",
			wantCalled:        true,
		},
		{
			name:              "non-allowed event listener drops",
			eventListenerName: "bitbucket-listener",
			wantCalled:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner.called = false

			event := domain.Event{
				Resource:          domain.ResourceEventListener,
				EventListenerName: tt.eventListenerName,
			}

			err := handler.Handle(context.Background(), event)
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}

			if inner.called != tt.wantCalled {
				t.Errorf("expected called=%v, got %v", tt.wantCalled, inner.called)
			}
		})
	}
}

func TestFilteredHandler_EmptyName(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline},
		},
	}
	handler := NewFilteredHandler(inner, cfg)

	// Event with empty pipeline name should pass through (cannot filter)
	event := domain.Event{
		Resource:     domain.ResourcePipelineRun,
		PipelineName: "",
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called for empty name (cannot filter)")
	}
}

func TestFilteredHandler_UnknownResourceType(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
	cfg := &config.ActionFilterConfig{
		Pipelines: config.FilterList{
			Allow: []string{buildPipeline},
		},
	}
	handler := NewFilteredHandler(inner, cfg)

	// Unknown resource type should pass through
	event := domain.Event{
		Resource: "unknown-resource-type",
	}

	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if !inner.called {
		t.Error("expected inner handler to be called for unknown resource type")
	}
}

func TestFilteredHandler_NameTypeDelegation(t *testing.T) {
	inner := &mockActionHandler{
		name: "github-handler",
		typ:  ActionPRComment,
	}
	handler := NewFilteredHandler(inner, nil)

	if handler.Name() != "github-handler" {
		t.Errorf("expected name=github-handler, got %s", handler.Name())
	}

	if handler.Type() != ActionPRComment {
		t.Errorf("expected type=pr_comment, got %s", handler.Type())
	}
}

func TestFilteredHandler_MultipleResourceTypes(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: ActionCommitStatus}
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
	handler := NewFilteredHandler(inner, cfg)

	tests := []struct {
		name       string
		event      domain.Event
		wantCalled bool
	}{
		{
			name: "allowed task passes",
			event: domain.Event{
				Resource: domain.ResourceTaskRun,
				TaskName: unitTest,
			},
			wantCalled: true,
		},
		{
			name: "non-allowed task drops",
			event: domain.Event{
				Resource: domain.ResourceTaskRun,
				TaskName: "integration-test",
			},
			wantCalled: false,
		},
		{
			name: "allowed pipeline passes",
			event: domain.Event{
				Resource:     domain.ResourcePipelineRun,
				PipelineName: buildPipeline,
			},
			wantCalled: true,
		},
		{
			name: "non-allowed pipeline drops",
			event: domain.Event{
				Resource:     domain.ResourcePipelineRun,
				PipelineName: deployPipeline,
			},
			wantCalled: false,
		},
		{
			name: "non-denied custom run passes",
			event: domain.Event{
				Resource: domain.ResourceCustomRun,
				TaskName: "approval-task",
			},
			wantCalled: true,
		},
		{
			name: "denied custom run drops",
			event: domain.Event{
				Resource: domain.ResourceCustomRun,
				TaskName: "skip-custom",
			},
			wantCalled: false,
		},
		{
			name: "allowed event listener passes",
			event: domain.Event{
				Resource:          domain.ResourceEventListener,
				EventListenerName: "github-listener",
			},
			wantCalled: true,
		},
		{
			name: "non-allowed event listener drops",
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
				t.Fatalf("expected nil error, got %v", err)
			}

			if inner.called != tt.wantCalled {
				t.Errorf("expected called=%v, got %v", tt.wantCalled, inner.called)
			}
		})
	}
}
