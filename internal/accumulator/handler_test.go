package accumulator

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	testOrg      = "org"
	testProvider = "github"
	testRepo     = "repo"
)

// mockActionHandler records calls to Handle for assertions.
type mockActionHandler struct {
	name   string
	typ    notifier.ActionType
	events []domain.Event
}

func (m *mockActionHandler) Name() string              { return m.name }
func (m *mockActionHandler) Type() notifier.ActionType { return m.typ }
func (m *mockActionHandler) Handle(_ context.Context, e domain.Event) error {
	m.events = append(m.events, e)
	return nil
}
func (m *mockActionHandler) Close() error { return nil }

func newTestHandler(t *testing.T) (*Handler, *mockActionHandler) {
	t.Helper()
	mock := &mockActionHandler{name: "mock-pr", typ: notifier.ActionPRComment}
	log := zaptest.NewLogger(t)
	buf := NewLRUBuffer(30*time.Second, 100)
	h := NewHandler("test-accumulator", mock, buf, log)
	t.Cleanup(func() { _ = h.Close() })
	return h, mock
}

func TestHandler_ImplementsActionHandler(t *testing.T) {
	h, _ := newTestHandler(t)
	var _ notifier.ActionHandler = h
	_ = h
}

func TestHandler_NameAndType(t *testing.T) {
	h, _ := newTestHandler(t)
	if h.Name() != "test-accumulator" {
		t.Errorf("expected name test-accumulator, got %s", h.Name())
	}
	if h.Type() != notifier.ActionPRComment {
		t.Errorf("expected type %s, got %s", notifier.ActionPRComment, h.Type())
	}
}

func TestHandler_SkipsNonPipelineRun(t *testing.T) {
	h, mock := newTestHandler(t)

	err := h.Handle(context.Background(), domain.Event{
		Resource: domain.ResourceTaskRun,
		RunID:    "uid-123",
		RunName:  testTaskBuild,
		State:    domain.StateSuccess,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.events) != 0 {
		t.Error("provider should not be called for TaskRun events")
	}
}

func TestHandler_SkipsEmptyUID(t *testing.T) {
	h, mock := newTestHandler(t)

	err := h.Handle(context.Background(), domain.Event{
		Resource: domain.ResourcePipelineRun,
		RunID:    "",
		State:    domain.StateRunning,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.events) != 0 {
		t.Error("provider should not be called for empty UID")
	}
}

func TestHandler_AccumulatesNonTerminal(t *testing.T) {
	h, mock := newTestHandler(t)

	// Add a TaskRun event
	err := h.Handle(context.Background(), domain.Event{
		Resource: domain.ResourceTaskRun,
		RunID:    "uid-abc",
		RunName:  testTaskBuild,
		State:    domain.StateRunning,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.events) != 0 {
		t.Error("non-terminal state should not trigger provider")
	}

	// Verify buffered
	state, exists := h.buffer.Get("uid-abc")
	if !exists {
		t.Fatal("expected uid-abc in buffer")
	}
	if len(state.Tasks) != 1 {
		t.Errorf("expected 1 task in buffer, got %d", len(state.Tasks))
	}
}

func TestHandler_FlushesOnTerminalState(t *testing.T) {
	h, mock := newTestHandler(t)
	ctx := context.Background()
	uid := "uid-pipeline-1"
	prNum := 42

	// Accumulate TaskRun events
	if err := h.Handle(ctx, domain.Event{
		Resource: domain.ResourceTaskRun,
		RunID:    uid,
		RunName:  testTaskBuild,
		State:    domain.StateRunning,
		Repo:     domain.Repo{Owner: testOrg, Name: testRepo},
		PRNumber: &prNum,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := h.Handle(ctx, domain.Event{
		Resource: domain.ResourceTaskRun,
		RunID:    uid,
		RunName:  testTaskTest,
		State:    domain.StateRunning,
		Repo:     domain.Repo{Owner: testOrg, Name: testRepo},
		PRNumber: &prNum,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Terminal event triggers flush
	if err := h.Handle(ctx, domain.Event{
		Resource:  domain.ResourcePipelineRun,
		RunID:     uid,
		RunName:   "pipeline-final",
		State:     domain.StateSuccess,
		CommitSHA: "abc123",
		Repo:      domain.Repo{Owner: testOrg, Name: testRepo},
		PRNumber:  &prNum,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.events) != 1 {
		t.Fatalf("expected 1 provider call, got %d", len(mock.events))
	}

	evt := mock.events[0]
	if evt.State != domain.StateSuccess {
		t.Errorf("expected state Success, got %v", evt.State)
	}
	if evt.CommitSHA != "abc123" {
		t.Errorf("expected commitSHA abc123, got %s", evt.CommitSHA)
	}
	if evt.Repo.Owner != testOrg {
		t.Errorf("expected repo owner org, got %s", evt.Repo.Owner)
	}
	if evt.Repo.Name != testRepo {
		t.Errorf("expected repo name repo, got %s", evt.Repo.Name)
	}
	if evt.PRNumber == nil || *evt.PRNumber != 42 {
		t.Errorf("expected PRNumber 42, got %v", evt.PRNumber)
	}
	if !strings.Contains(evt.Description, "Pipeline Summary") {
		t.Error("expected description to contain Pipeline Summary")
	}
	if evt.Context != "tekton/pipeline-summary" {
		t.Errorf("expected context tekton/pipeline-summary, got %s", evt.Context)
	}

	// Buffer should be empty after flush
	_, exists := h.buffer.Get(uid)
	if exists {
		t.Error("expected buffer to be empty after flush")
	}
}

func TestHandler_AllTerminalStatesTriggerFlush(t *testing.T) {
	terminalStates := []domain.State{
		domain.StateSuccess,
		domain.StateFailure,
		domain.StateCanceled,
		domain.StateError,
	}

	for _, state := range terminalStates {
		t.Run(string(state), func(t *testing.T) {
			h, mock := newTestHandler(t)
			ctx := context.Background()
			uid := "uid-" + string(state)

			// Add a TaskRun first
			if err := h.Handle(ctx, domain.Event{
				Resource: domain.ResourceTaskRun,
				RunID:    uid,
				RunName:  "task-1",
				State:    domain.StateRunning,
			}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Then terminal PipelineRun triggers flush
			if err := h.Handle(ctx, domain.Event{
				Resource: domain.ResourcePipelineRun,
				RunID:    uid,
				RunName:  "pipeline-run-1",
				State:    state,
			}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(mock.events) != 1 {
				t.Errorf("expected 1 provider call for state %s, got %d", state, len(mock.events))
			}
		})
	}
}

func TestGenerateMarkdown(t *testing.T) {
	now := time.Now()
	state := &RunState{
		UID: "test-uid",
		Tasks: map[string]*domain.Event{
			"build": {
				State:      domain.StateSuccess,
				StartedAt:  now.Add(-30 * time.Second),
				FinishedAt: now,
			},
			"test": {
				State:      domain.StateFailure,
				StartedAt:  now.Add(-20 * time.Second),
				FinishedAt: now,
			},
		},
	}

	md := generateMarkdown(state)

	checks := []string{"## Pipeline Summary", "| Task | Status | Duration |", "build", "test", "✅", "❌"}
	for _, check := range checks {
		if !strings.Contains(md, check) {
			t.Errorf("expected markdown to contain %q", check)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	now := time.Now()

	if got := formatDuration(time.Time{}, now); got != notAvailable {
		t.Errorf("expected N/A for zero start, got %s", got)
	}
	if got := formatDuration(now, time.Time{}); got != notAvailable {
		t.Errorf("expected N/A for zero finish, got %s", got)
	}
	if got := formatDuration(now.Add(-90*time.Second), now); got != "1m30s" {
		t.Errorf("expected 1m30s, got %s", got)
	}
}

func TestIsTerminalState(t *testing.T) {
	terminal := []domain.State{domain.StateSuccess, domain.StateFailure, domain.StateCanceled, domain.StateError}
	for _, s := range terminal {
		if !isTerminalState(s) {
			t.Errorf("expected %s to be terminal", s)
		}
	}

	nonTerminal := []domain.State{domain.StatePending, domain.StateRunning}
	for _, s := range nonTerminal {
		if isTerminalState(s) {
			t.Errorf("expected %s to be non-terminal", s)
		}
	}
}

func TestAccumulatorCloser(t *testing.T) {
	mock := &mockActionHandler{name: "mock-pr", typ: notifier.ActionPRComment}
	log := zaptest.NewLogger(t)
	buf := NewLRUBuffer(30*time.Second, 100)
	h := NewHandler("test-accumulator", mock, buf, log)

	if err := h.Handle(context.Background(), domain.Event{
		Resource: domain.ResourceTaskRun,
		RunID:    testUID,
		RunName:  "task-1",
		State:    domain.StateRunning,
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if err := h.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	_, exists := buf.Get(testUID)
	if exists {
		t.Error("expected buffer to be empty after Close")
	}

	if err := h.Close(); err != nil {
		t.Fatalf("second Close() returned error: %v", err)
	}
}

func TestHandler_MultiPipelineRunGrouping(t *testing.T) {
	h, mock := newTestHandler(t)
	ctx := context.Background()
	groupID := "deploy-group-1"

	taskEvents := []domain.Event{
		{Resource: domain.ResourceTaskRun, RunID: testUID, RunName: testTaskBuild, State: domain.StateRunning, AccumulatorGroupID: groupID},
		{Resource: domain.ResourceTaskRun, RunID: "uid-2", RunName: testTaskTest, State: domain.StateRunning, AccumulatorGroupID: groupID},
		{Resource: domain.ResourceTaskRun, RunID: "uid-3", RunName: "task-deploy", State: domain.StateRunning, AccumulatorGroupID: groupID},
	}
	for _, evt := range taskEvents {
		if err := h.Handle(ctx, evt); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if len(mock.events) != 0 {
		t.Error("provider should not be called for non-terminal events")
	}

	pr1 := domain.Event{
		Resource:           domain.ResourcePipelineRun,
		RunID:              testUID,
		RunName:            "pipeline-1",
		State:              domain.StateSuccess,
		AccumulatorGroupID: groupID,
		Provider:           testProvider,
		Repo:               domain.Repo{Owner: testOrg, Name: testRepo},
	}
	if err := h.Handle(ctx, pr1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.events) != 0 {
		t.Error("provider should not be called when group is incomplete")
	}

	pr2 := domain.Event{
		Resource:           domain.ResourcePipelineRun,
		RunID:              "uid-2",
		RunName:            "pipeline-2",
		State:              domain.StateFailure,
		AccumulatorGroupID: groupID,
		Provider:           testProvider,
		Repo:               domain.Repo{Owner: testOrg, Name: testRepo},
	}
	if err := h.Handle(ctx, pr2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.events) != 0 {
		t.Error("provider should not be called when group is still incomplete")
	}

	pr3 := domain.Event{
		Resource:           domain.ResourcePipelineRun,
		RunID:              "uid-3",
		RunName:            "pipeline-3",
		State:              domain.StateSuccess,
		AccumulatorGroupID: groupID,
		Provider:           testProvider,
		Repo:               domain.Repo{Owner: testOrg, Name: testRepo},
	}
	if err := h.Handle(ctx, pr3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.events) != 1 {
		t.Fatalf("expected 1 provider call when all group members are terminal, got %d", len(mock.events))
	}

	evt := mock.events[0]
	if evt.State != domain.StateSuccess {
		t.Errorf("expected state Success, got %v", evt.State)
	}
	if !strings.Contains(evt.Description, "Pipeline Summary") {
		t.Error("expected description to contain Pipeline Summary")
	}
	if !strings.Contains(evt.Description, testTaskBuild) {
		t.Error("expected description to contain task-build")
	}
	if !strings.Contains(evt.Description, testTaskTest) {
		t.Error("expected description to contain task-test")
	}
	if !strings.Contains(evt.Description, "task-deploy") {
		t.Error("expected description to contain task-deploy")
	}
}

func TestHandler_SingleRunBackwardCompat(t *testing.T) {
	h, mock := newTestHandler(t)
	ctx := context.Background()

	if err := h.Handle(ctx, domain.Event{
		Resource: domain.ResourceTaskRun,
		RunID:    testUID,
		RunName:  testTaskBuild,
		State:    domain.StateRunning,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := h.Handle(ctx, domain.Event{
		Resource: domain.ResourcePipelineRun,
		RunID:    testUID,
		RunName:  "pipeline-1",
		State:    domain.StateSuccess,
		Provider: testProvider,
		Repo:     domain.Repo{Owner: testOrg, Name: testRepo},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.events) != 1 {
		t.Fatalf("expected 1 provider call for single run, got %d", len(mock.events))
	}
	if mock.events[0].State != domain.StateSuccess {
		t.Errorf("expected state Success, got %v", mock.events[0].State)
	}
}

func TestHandler_GroupTaskRunsAccumulateAcrossUIDs(t *testing.T) {
	h, mock := newTestHandler(t)
	ctx := context.Background()
	groupID := "group-multi"

	taskEvents := []domain.Event{
		{Resource: domain.ResourceTaskRun, RunID: testUID, RunName: testTaskBuild, State: domain.StateSuccess, AccumulatorGroupID: groupID},
		{Resource: domain.ResourceTaskRun, RunID: "uid-2", RunName: testTaskTest, State: domain.StateSuccess, AccumulatorGroupID: groupID},
		{Resource: domain.ResourceTaskRun, RunID: testUID, RunName: "task-lint", State: domain.StateSuccess, AccumulatorGroupID: groupID},
	}
	for _, evt := range taskEvents {
		if err := h.Handle(ctx, evt); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if err := h.Handle(ctx, domain.Event{
		Resource:           domain.ResourcePipelineRun,
		RunID:              testUID,
		RunName:            "pipeline-1",
		State:              domain.StateSuccess,
		AccumulatorGroupID: groupID,
		Provider:           testProvider,
		Repo:               domain.Repo{Owner: testOrg, Name: testRepo},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := h.Handle(ctx, domain.Event{
		Resource:           domain.ResourcePipelineRun,
		RunID:              "uid-2",
		RunName:            "pipeline-2",
		State:              domain.StateSuccess,
		AccumulatorGroupID: groupID,
		Provider:           testProvider,
		Repo:               domain.Repo{Owner: testOrg, Name: testRepo},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.events) != 1 {
		t.Fatalf("expected 1 provider call, got %d", len(mock.events))
	}

	desc := mock.events[0].Description
	if !strings.Contains(desc, testTaskBuild) {
		t.Error("expected merged description to contain task-build")
	}
	if !strings.Contains(desc, testTaskTest) {
		t.Error("expected merged description to contain task-test")
	}
	if !strings.Contains(desc, "task-lint") {
		t.Error("expected merged description to contain task-lint")
	}
}
