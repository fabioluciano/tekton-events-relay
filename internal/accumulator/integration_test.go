//go:build integration
// +build integration

package accumulator

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// mockProviderHandler is a spy that records Handle calls and captures the comment markdown.
type mockProviderHandler struct {
	name         string
	callCount    int
	lastEvent    *domain.Event
	lastMarkdown string
}

func (m *mockProviderHandler) Name() string {
	return m.name
}

func (m *mockProviderHandler) Type() notifier.ActionType {
	return notifier.ActionPRComment
}

func (m *mockProviderHandler) Handle(_ context.Context, e domain.Event) error {
	m.callCount++
	m.lastEvent = &e
	m.lastMarkdown = e.Description
	return nil
}

// TestAccumulatorFlush_Integration tests the complete accumulator flow:
// 1. Add multiple TaskRun events
// 2. Add terminal PipelineRun event
// 3. Verify flush is called with aggregate markdown table
// 4. Verify comment posted with correct task summaries
func TestAccumulatorFlush_Integration(t *testing.T) {
	logger := zap.NewNop()

	// Setup mock SCM provider
	mockProvider := &mockProviderHandler{name: "github"}

	// Create accumulator handler
	buf := NewLRUBuffer(30*time.Second, 100)
	handler := NewHandler("test-accumulator", mockProvider, buf, logger)
	defer handler.Close()

	pipelineUID := "pipeline-uid-123"
	prNum := 42

	// Add 3 TaskRun events
	task1 := domain.Event{
		Provider:   "github",
		Resource:   domain.ResourceTaskRun,
		TaskName:   "build",
		RunName:    "build-run-1",
		RunID:      pipelineUID,
		Namespace:  "default",
		State:      domain.StateSuccess,
		StartedAt:  time.Now().Add(-5 * time.Minute),
		FinishedAt: time.Now().Add(-3 * time.Minute),
		CommitSHA:  "abc123",
		Repo: domain.Repo{
			Owner: "myorg",
			Name:  "myrepo",
		},
		PRNumber: &prNum,
	}

	task2 := domain.Event{
		Provider:   "github",
		Resource:   domain.ResourceTaskRun,
		TaskName:   "test",
		RunName:    "test-run-1",
		RunID:      pipelineUID,
		Namespace:  "default",
		State:      domain.StateFailure,
		StartedAt:  time.Now().Add(-3 * time.Minute),
		FinishedAt: time.Now().Add(-1 * time.Minute),
		CommitSHA:  "abc123",
		Repo: domain.Repo{
			Owner: "myorg",
			Name:  "myrepo",
		},
		PRNumber: &prNum,
	}

	task3 := domain.Event{
		Provider:   "github",
		Resource:   domain.ResourceTaskRun,
		TaskName:   "deploy",
		RunName:    "deploy-run-1",
		RunID:      pipelineUID,
		Namespace:  "default",
		State:      domain.StateSuccess,
		StartedAt:  time.Now().Add(-2 * time.Minute),
		FinishedAt: time.Now().Add(-30 * time.Second),
		CommitSHA:  "abc123",
		Repo: domain.Repo{
			Owner: "myorg",
			Name:  "myrepo",
		},
		PRNumber: &prNum,
	}

	// Process TaskRun events (should accumulate, not post)
	ctx := context.Background()
	err := handler.Handle(ctx, task1)
	if err != nil {
		t.Fatalf("task1 handle failed: %v", err)
	}

	err = handler.Handle(ctx, task2)
	if err != nil {
		t.Fatalf("task2 handle failed: %v", err)
	}

	err = handler.Handle(ctx, task3)
	if err != nil {
		t.Fatalf("task3 handle failed: %v", err)
	}

	// Verify no comments posted yet
	if mockProvider.callCount != 0 {
		t.Errorf("expected 0 comments before terminal event, got %d", mockProvider.callCount)
	}

	// Add terminal PipelineRun event (should trigger flush)
	pipelineEvent := domain.Event{
		Provider:     "github",
		Resource:     domain.ResourcePipelineRun,
		PipelineName: "ci-pipeline",
		RunName:      "ci-pipeline-run-1",
		RunID:        pipelineUID,
		Namespace:    "default",
		State:        domain.StateFailure, // terminal state
		CommitSHA:    "abc123",
		Repo: domain.Repo{
			Owner: "myorg",
			Name:  "myrepo",
		},
		PRNumber: &prNum,
	}

	err = handler.Handle(ctx, pipelineEvent)
	if err != nil {
		t.Fatalf("pipeline handle failed: %v", err)
	}

	// Verify comment was posted
	if mockProvider.callCount != 1 {
		t.Errorf("expected 1 comment after terminal event, got %d", mockProvider.callCount)
	}

	// Verify markdown table generated correctly
	markdown := mockProvider.lastMarkdown

	// Check for table structure
	if !strings.Contains(markdown, "## Pipeline Summary") {
		t.Error("expected markdown to contain '## Pipeline Summary'")
	}

	if !strings.Contains(markdown, "| Task | Status | Duration |") {
		t.Error("expected markdown to contain table header")
	}

	if !strings.Contains(markdown, "|------|--------|----------|") {
		t.Error("expected markdown to contain table separator")
	}

	// Check for all tasks in table
	expectedTasks := []string{"build", "test", "deploy"}
	for _, task := range expectedTasks {
		if !strings.Contains(markdown, task) {
			t.Errorf("expected markdown to contain task '%s'", task)
		}
	}

	// Check for state emojis
	if !strings.Contains(markdown, "✅") {
		t.Error("expected markdown to contain success emoji ✅")
	}

	if !strings.Contains(markdown, "❌") {
		t.Error("expected markdown to contain failure emoji ❌")
	}

	// Verify event fields
	if mockProvider.lastEvent.Context != "tekton/pipeline-summary" {
		t.Errorf("expected context='tekton/pipeline-summary', got '%s'", mockProvider.lastEvent.Context)
	}

	if mockProvider.lastEvent.State != domain.StateFailure {
		t.Errorf("expected state=failure, got '%s'", mockProvider.lastEvent.State)
	}

	if mockProvider.lastEvent.PRNumber == nil || *mockProvider.lastEvent.PRNumber != 42 {
		var val int
		if mockProvider.lastEvent.PRNumber != nil {
			val = *mockProvider.lastEvent.PRNumber
		}
		t.Errorf("expected PRNumber=42, got %v", val)
	}
}

// TestAccumulatorNonTerminalPipelineRun_Integration tests that non-terminal PipelineRun events
// accumulate but don't trigger flush.
func TestAccumulatorNonTerminalPipelineRun_Integration(t *testing.T) {
	logger := zap.NewNop()
	mockProvider := &mockProviderHandler{name: "github"}
	buf := NewLRUBuffer(30*time.Second, 100)
	handler := NewHandler("test-accumulator", mockProvider, buf, logger)
	defer handler.Close()

	pipelineUID := "pipeline-uid-456"

	// Add running PipelineRun event (non-terminal)
	runningPipeline := domain.Event{
		Provider:     "github",
		Resource:     domain.ResourcePipelineRun,
		PipelineName: "ci-pipeline",
		RunName:      "ci-pipeline-run-2",
		RunID:        pipelineUID,
		Namespace:    "default",
		State:        domain.StateRunning, // non-terminal
		CommitSHA:    "def456",
		Repo: domain.Repo{
			Owner: "myorg",
			Name:  "myrepo",
		},
	}

	ctx := context.Background()
	err := handler.Handle(ctx, runningPipeline)
	if err != nil {
		t.Fatalf("running pipeline handle failed: %v", err)
	}

	// Verify no comment posted
	if mockProvider.callCount != 0 {
		t.Errorf("expected 0 comments for non-terminal pipeline, got %d", mockProvider.callCount)
	}
}

// TestAccumulatorEmptyBuffer_Integration tests that terminal PipelineRun with no accumulated
// tasks doesn't post a comment.
func TestAccumulatorEmptyBuffer_Integration(t *testing.T) {
	logger := zap.NewNop()
	mockProvider := &mockProviderHandler{name: "github"}
	buf := NewLRUBuffer(30*time.Second, 100)
	handler := NewHandler("test-accumulator", mockProvider, buf, logger)
	defer handler.Close()

	// Add terminal PipelineRun with no prior TaskRuns
	pipelineEvent := domain.Event{
		Provider:     "github",
		Resource:     domain.ResourcePipelineRun,
		PipelineName: "ci-pipeline",
		RunName:      "ci-pipeline-run-3",
		RunID:        "pipeline-uid-789",
		Namespace:    "default",
		State:        domain.StateSuccess, // terminal
		CommitSHA:    "ghi789",
		Repo: domain.Repo{
			Owner: "myorg",
			Name:  "myrepo",
		},
	}

	ctx := context.Background()
	err := handler.Handle(ctx, pipelineEvent)
	if err != nil {
		t.Fatalf("empty pipeline handle failed: %v", err)
	}

	// Verify no comment posted (no tasks to summarize)
	if mockProvider.callCount != 0 {
		t.Errorf("expected 0 comments for empty buffer, got %d", mockProvider.callCount)
	}
}

// TestAccumulatorMultiplePipelines_Integration tests that accumulator correctly
// isolates events by PipelineRun UID.
func TestAccumulatorMultiplePipelines_Integration(t *testing.T) {
	logger := zap.NewNop()
	mockProvider := &mockProviderHandler{name: "github"}
	buf := NewLRUBuffer(30*time.Second, 100)
	handler := NewHandler("test-accumulator", mockProvider, buf, logger)
	defer handler.Close()

	pipeline1UID := "pipeline-uid-aaa"
	pipeline2UID := "pipeline-uid-bbb"

	ctx := context.Background()

	// Add task to pipeline 1
	task1 := domain.Event{
		Provider:   "github",
		Resource:   domain.ResourceTaskRun,
		TaskName:   "build-p1",
		RunName:    "build-p1-run",
		RunID:      pipeline1UID,
		Namespace:  "default",
		State:      domain.StateSuccess,
		StartedAt:  time.Now().Add(-5 * time.Minute),
		FinishedAt: time.Now().Add(-3 * time.Minute),
	}

	// Add task to pipeline 2
	task2 := domain.Event{
		Provider:   "github",
		Resource:   domain.ResourceTaskRun,
		TaskName:   "build-p2",
		RunName:    "build-p2-run",
		RunID:      pipeline2UID,
		Namespace:  "default",
		State:      domain.StateSuccess,
		StartedAt:  time.Now().Add(-5 * time.Minute),
		FinishedAt: time.Now().Add(-3 * time.Minute),
	}

	err := handler.Handle(ctx, task1)
	if err != nil {
		t.Fatalf("task1 handle failed: %v", err)
	}

	err = handler.Handle(ctx, task2)
	if err != nil {
		t.Fatalf("task2 handle failed: %v", err)
	}

	// Complete pipeline 1
	pipeline1Done := domain.Event{
		Provider:     "github",
		Resource:     domain.ResourcePipelineRun,
		PipelineName: "ci-pipeline-1",
		RunName:      "ci-pipeline-1-run",
		RunID:        pipeline1UID,
		Namespace:    "default",
		State:        domain.StateSuccess,
	}

	err = handler.Handle(ctx, pipeline1Done)
	if err != nil {
		t.Fatalf("pipeline1 handle failed: %v", err)
	}

	// Verify 1 comment posted
	if mockProvider.callCount != 1 {
		t.Errorf("expected 1 comment after pipeline1 completion, got %d", mockProvider.callCount)
	}

	// Verify markdown contains only pipeline 1 task
	if !strings.Contains(mockProvider.lastMarkdown, "build-p1") {
		t.Error("expected markdown to contain build-p1")
	}

	if strings.Contains(mockProvider.lastMarkdown, "build-p2") {
		t.Error("expected markdown NOT to contain build-p2")
	}

	// Complete pipeline 2
	pipeline2Done := domain.Event{
		Provider:     "github",
		Resource:     domain.ResourcePipelineRun,
		PipelineName: "ci-pipeline-2",
		RunName:      "ci-pipeline-2-run",
		RunID:        pipeline2UID,
		Namespace:    "default",
		State:        domain.StateSuccess,
	}

	err = handler.Handle(ctx, pipeline2Done)
	if err != nil {
		t.Fatalf("pipeline2 handle failed: %v", err)
	}

	// Verify 2 comments posted total
	if mockProvider.callCount != 2 {
		t.Errorf("expected 2 comments after pipeline2 completion, got %d", mockProvider.callCount)
	}

	// Verify markdown contains only pipeline 2 task
	if !strings.Contains(mockProvider.lastMarkdown, "build-p2") {
		t.Error("expected markdown to contain build-p2")
	}

	if strings.Contains(mockProvider.lastMarkdown, "build-p1") {
		t.Error("expected markdown NOT to contain build-p1 (from previous pipeline)")
	}
}
