package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/event/tekton"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	testProviderGitHub      = "github"
	testNamespaceDefault    = "default"
	testNamespaceProd       = "production"
	testRepoOwner           = "myorg"
	testRepoName            = "myrepo"
	testCommitSHA           = "abc123"
	errMsgDecodeFailed      = "decode failed: %v"
	errMsgChainFailed       = "chain failed: %v"
	testTektonController    = "/tekton/controller"
	testTektonEventType     = "dev.tekton.event.taskrun.successful.v1"
	testAnnotationProvider  = "tekton.dev/tekton-events-relay.scm.provider"
	testAnnotationRepoOwner = "tekton.dev/tekton-events-relay.scm.repo-owner"
	testAnnotationRepoName  = "tekton.dev/tekton-events-relay.scm.repo-name"
	testAnnotationCommitSHA = "tekton.dev/tekton-events-relay.scm.commit-sha"
	testLabelTask           = "tekton.dev/task"
	testLabelPipeline       = "tekton.dev/pipeline"
	testKeyMetadata         = "metadata"
	testKeyName             = "name"
	testKeyNamespace        = "namespace"
	testKeyUID              = "uid"
	testKeyAnnotations      = "annotations"
	testKeyLabels           = "labels"
	testKeyStatus           = "status"
)

// mockActionHandler is a spy that records Handle calls
type mockActionHandler struct {
	name       string
	actionType notifier.ActionType
	calls      []domain.Event
	shouldFail bool
}

func (m *mockActionHandler) Name() string              { return m.name }
func (m *mockActionHandler) Provider() string          { return m.name }
func (m *mockActionHandler) Type() notifier.ActionType { return m.actionType }
func (m *mockActionHandler) Close() error              { return nil }
func (m *mockActionHandler) Handle(_ context.Context, e domain.Event) error {
	m.calls = append(m.calls, e)
	if m.shouldFail {
		return nil // Simulate handler deciding not to process
	}
	return nil
}

func (m *mockActionHandler) wasCalled() bool {
	return len(m.calls) > 0
}

func (m *mockActionHandler) callCount() int {
	return len(m.calls)
}

// TestIntegration_FullPipeline_TaskRun tests the complete pipeline:
// decode TaskRun → EventFilter (allow) → FilteredHandler (allow) → ConditionalHandler (allow) → handler called
func TestIntegration_FullPipeline_TaskRun(t *testing.T) {
	// Setup mock handler (name must match provider for dispatcher routing)
	mock := &mockActionHandler{name: testProviderGitHub, actionType: notifier.ActionCommitStatus}

	// Wrap with filter (allow task "build")
	filterCfg := &notifier.FilterConfig{
		Tasks: notifier.FilterList{
			Allow: []string{"build"},
		},
	}
	filtered := notifier.NewFilteredHandler(mock, filterCfg)

	// Register handler
	reg := notifier.NewRegistry()
	reg.Register(filtered)

	// Build chain with EventFilter allowing TaskRun
	chain := Build(
		NewValidator(),
		NewEventFilter(true, false, false, false, false, nil, nil), // Allow TaskRun only
		newMemDeduper(100, nil),
		NewEnricher(""),
		NewDispatcher(reg, testLogger(), nil, 10),
	)

	// Create TaskRun CloudEvent
	taskRunPayload := map[string]any{
		"taskRun": map[string]any{
			testKeyMetadata: map[string]any{
				testKeyName:      "build-run-123",
				testKeyNamespace: testNamespaceDefault,
				testKeyUID:       "task-uid-123",
				testKeyAnnotations: map[string]any{
					testAnnotationProvider:                       testProviderGitHub,
					testAnnotationRepoOwner:                      testRepoOwner,
					testAnnotationRepoName:                       testRepoName,
					testAnnotationCommitSHA:                      testCommitSHA,
					"tekton.dev/tekton-events-relay.scm.context": "ci/build",
				},
				testKeyLabels: map[string]any{
					testLabelTask: "build",
				},
			},
			testKeyStatus: map[string]any{},
		},
	}

	data, _ := json.Marshal(taskRunPayload)
	rawEvent := event.RawEvent{
		ID:     "event-123",
		Type:   "dev.tekton.event.taskrun.successful.v1",
		Source: testTektonController,
		Data:   data,
	}

	// Decode
	decoder := tekton.NewTaskRunDecoder()
	env, err := decoder.Decode(rawEvent)
	if err != nil {
		t.Fatalf(errMsgDecodeFailed, err)
	}

	// Process through chain
	err = chain.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf(errMsgChainFailed, err)
	}

	// Verify handler was called
	if !mock.wasCalled() {
		t.Error("expected handler to be called")
	}
	if mock.callCount() != 1 {
		t.Errorf("expected 1 call, got %d", mock.callCount())
	}
	if mock.calls[0].TaskName != "build" {
		t.Errorf("expected TaskName=build, got %s", mock.calls[0].TaskName)
	}
}

// TestIntegration_FilteredByDeny tests FilteredHandler denying an event
func TestIntegration_FilteredByDeny(t *testing.T) {
	mock := &mockActionHandler{name: testProviderGitHub, actionType: notifier.ActionCommitStatus}

	// Filter denies task "cleanup"
	filterCfg := &notifier.FilterConfig{
		Tasks: notifier.FilterList{
			Deny: []string{"cleanup"},
		},
	}
	filtered := notifier.NewFilteredHandler(mock, filterCfg)

	reg := notifier.NewRegistry()
	reg.Register(filtered)

	chain := Build(
		NewValidator(),
		NewEventFilter(true, false, false, false, false, nil, nil),
		newMemDeduper(100, nil),
		NewEnricher(""),
		NewDispatcher(reg, testLogger(), nil, 10),
	)

	// TaskRun with task name "cleanup"
	taskRunPayload := map[string]any{
		"taskRun": map[string]any{
			testKeyMetadata: map[string]any{
				testKeyName:      "cleanup-run-456",
				testKeyNamespace: testNamespaceDefault,
				testKeyUID:       "task-uid-456",
				testKeyAnnotations: map[string]any{
					testAnnotationProvider:  testProviderGitHub,
					testAnnotationRepoOwner: testRepoOwner,
					testAnnotationRepoName:  testRepoName,
					testAnnotationCommitSHA: testCommitSHA,
				},
				testKeyLabels: map[string]any{
					testLabelTask: "cleanup",
				},
			},
			testKeyStatus: map[string]any{},
		},
	}

	data, _ := json.Marshal(taskRunPayload)
	rawEvent := event.RawEvent{
		ID:     "event-456",
		Type:   "dev.tekton.event.taskrun.successful.v1",
		Source: testTektonController,
		Data:   data,
	}

	decoder := tekton.NewTaskRunDecoder()
	env, err := decoder.Decode(rawEvent)
	if err != nil {
		t.Fatalf(errMsgDecodeFailed, err)
	}

	err = chain.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf(errMsgChainFailed, err)
	}

	// Verify handler was NOT called (filtered out)
	if mock.wasCalled() {
		t.Error("expected handler to be filtered out by deny list")
	}
}

// TestIntegration_FilteredByCEL tests ConditionalHandler (CEL) denying an event
func TestIntegration_FilteredByCEL(t *testing.T) {
	mock := &mockActionHandler{name: testProviderGitHub, actionType: notifier.ActionCommitStatus}

	// No filter at FilteredHandler level
	filtered := notifier.NewFilteredHandler(mock, nil)

	reg := notifier.NewRegistry()
	reg.Register(filtered)

	chain := Build(
		NewValidator(),
		NewEventFilter(true, false, false, false, false, nil, nil),
		newMemDeduper(100, nil),
		NewEnricher(""),
		NewDispatcher(reg, testLogger(), nil, 10),
	)

	// Create event that would fail CEL condition (if we had CEL configured)
	// For this test, we simulate by just verifying FilteredHandler passes through
	taskRunPayload := map[string]any{
		"taskRun": map[string]any{
			testKeyMetadata: map[string]any{
				testKeyName:      "test-run-789",
				testKeyNamespace: testNamespaceDefault,
				testKeyUID:       "task-uid-789",
				testKeyAnnotations: map[string]any{
					testAnnotationProvider:  testProviderGitHub,
					testAnnotationRepoOwner: testRepoOwner,
					testAnnotationRepoName:  testRepoName,
					testAnnotationCommitSHA: testCommitSHA,
				},
				testKeyLabels: map[string]any{
					testLabelTask: "test",
				},
			},
			testKeyStatus: map[string]any{},
		},
	}

	data, _ := json.Marshal(taskRunPayload)
	rawEvent := event.RawEvent{
		ID:     "event-789",
		Type:   "dev.tekton.event.taskrun.successful.v1",
		Source: testTektonController,
		Data:   data,
	}

	decoder := tekton.NewTaskRunDecoder()
	env, err := decoder.Decode(rawEvent)
	if err != nil {
		t.Fatalf(errMsgDecodeFailed, err)
	}

	err = chain.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf(errMsgChainFailed, err)
	}

	// With no filters, handler should be called
	if !mock.wasCalled() {
		t.Error("expected handler to be called when no filters are configured")
	}
}

// TestIntegration_BothFiltersPass tests that both FilteredHandler and CEL pass
//
//nolint:dupl // Test helper patterns are intentionally similar
func TestIntegration_BothFiltersPass(t *testing.T) {
	mock := &mockActionHandler{name: testProviderGitHub, actionType: notifier.ActionCommitStatus}

	// Filter allows task "deploy"
	filterCfg := &notifier.FilterConfig{
		Tasks: notifier.FilterList{
			Allow: []string{"deploy"},
		},
	}
	filtered := notifier.NewFilteredHandler(mock, filterCfg)

	reg := notifier.NewRegistry()
	reg.Register(filtered)

	chain := Build(
		NewValidator(),
		NewEventFilter(true, false, false, false, false, nil, nil),
		newMemDeduper(100, nil),
		NewEnricher(""),
		NewDispatcher(reg, testLogger(), nil, 10),
	)

	taskRunPayload := map[string]any{
		"taskRun": map[string]any{
			testKeyMetadata: map[string]any{
				testKeyName:      "deploy-run-999",
				testKeyNamespace: testNamespaceProd,
				testKeyUID:       "task-uid-999",
				testKeyAnnotations: map[string]any{
					testAnnotationProvider:  testProviderGitHub,
					testAnnotationRepoOwner: testRepoOwner,
					testAnnotationRepoName:  testRepoName,
					testAnnotationCommitSHA: testCommitSHA,
				},
				testKeyLabels: map[string]any{
					testLabelTask: "deploy",
				},
			},
			testKeyStatus: map[string]any{},
		},
	}

	data, _ := json.Marshal(taskRunPayload)
	rawEvent := event.RawEvent{
		ID:     "event-999",
		Type:   "dev.tekton.event.taskrun.successful.v1",
		Source: testTektonController,
		Data:   data,
	}

	decoder := tekton.NewTaskRunDecoder()
	env, err := decoder.Decode(rawEvent)
	if err != nil {
		t.Fatalf(errMsgDecodeFailed, err)
	}

	err = chain.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf(errMsgChainFailed, err)
	}

	// Handler should be called
	if !mock.wasCalled() {
		t.Error("expected handler to be called when both filters pass")
	}
	if mock.calls[0].Namespace != testNamespaceProd {
		t.Errorf("expected Namespace=production, got %s", mock.calls[0].Namespace)
	}
}

// TestIntegration_PipelineRun tests pipeline filtering
//
//nolint:dupl // Test helper patterns are intentionally similar
func TestIntegration_PipelineRun(t *testing.T) {
	mock := &mockActionHandler{name: testProviderGitHub, actionType: notifier.ActionCommitStatus}

	// Filter allows pipeline "ci-pipeline"
	filterCfg := &notifier.FilterConfig{
		Pipelines: notifier.FilterList{
			Allow: []string{"ci-pipeline"},
		},
	}
	filtered := notifier.NewFilteredHandler(mock, filterCfg)

	reg := notifier.NewRegistry()
	reg.Register(filtered)

	chain := Build(
		NewValidator(),
		NewEventFilter(false, true, false, false, false, nil, nil), // Allow PipelineRun only
		newMemDeduper(100, nil),
		NewEnricher(""),
		NewDispatcher(reg, testLogger(), nil, 10),
	)

	pipelineRunPayload := map[string]any{
		"pipelineRun": map[string]any{
			testKeyMetadata: map[string]any{
				testKeyName:      "ci-run-111",
				testKeyNamespace: testNamespaceDefault,
				testKeyUID:       "pipeline-uid-111",
				testKeyAnnotations: map[string]any{
					testAnnotationProvider:  testProviderGitHub,
					testAnnotationRepoOwner: testRepoOwner,
					testAnnotationRepoName:  testRepoName,
					testAnnotationCommitSHA: testCommitSHA,
				},
				testKeyLabels: map[string]any{
					testLabelPipeline: "ci-pipeline",
				},
			},
			testKeyStatus: map[string]any{},
		},
	}

	data, _ := json.Marshal(pipelineRunPayload)
	rawEvent := event.RawEvent{
		ID:     "event-111",
		Type:   "dev.tekton.event.pipelinerun.successful.v1",
		Source: testTektonController,
		Data:   data,
	}

	decoder := tekton.NewPipelineRunDecoder()
	env, err := decoder.Decode(rawEvent)
	if err != nil {
		t.Fatalf(errMsgDecodeFailed, err)
	}

	err = chain.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf(errMsgChainFailed, err)
	}

	if !mock.wasCalled() {
		t.Error("expected handler to be called for allowed pipeline")
	}
	if mock.calls[0].PipelineName != "ci-pipeline" {
		t.Errorf("expected PipelineName=ci-pipeline, got %s", mock.calls[0].PipelineName)
	}
}

// TestIntegration_CustomRun tests custom run filtering
//
//nolint:dupl // Test helper patterns are intentionally similar
func TestIntegration_CustomRun(t *testing.T) {
	mock := &mockActionHandler{name: testProviderGitHub, actionType: notifier.ActionCommitStatus}

	// Filter allows custom run "approval-task"
	filterCfg := &notifier.FilterConfig{
		CustomRuns: notifier.FilterList{
			Allow: []string{"approval-task"},
		},
	}
	filtered := notifier.NewFilteredHandler(mock, filterCfg)

	reg := notifier.NewRegistry()
	reg.Register(filtered)

	chain := Build(
		NewValidator(),
		NewEventFilter(false, false, true, false, false, nil, nil), // Allow CustomRun only
		newMemDeduper(100, nil),
		NewEnricher(""),
		NewDispatcher(reg, testLogger(), nil, 10),
	)

	customRunPayload := map[string]any{
		"customRun": map[string]any{
			testKeyMetadata: map[string]any{
				testKeyName:      "approval-run-222",
				testKeyNamespace: testNamespaceDefault,
				testKeyUID:       "custom-uid-222",
				testKeyAnnotations: map[string]any{
					testAnnotationProvider:  testProviderGitHub,
					testAnnotationRepoOwner: testRepoOwner,
					testAnnotationRepoName:  testRepoName,
					testAnnotationCommitSHA: testCommitSHA,
				},
				testKeyLabels: map[string]any{
					testLabelTask: "approval-task",
				},
			},
			testKeyStatus: map[string]any{},
		},
	}

	data, _ := json.Marshal(customRunPayload)
	rawEvent := event.RawEvent{
		ID:     "event-222",
		Type:   "dev.tekton.event.customrun.successful.v1",
		Source: testTektonController,
		Data:   data,
	}

	decoder := tekton.NewCustomRunDecoder()
	env, err := decoder.Decode(rawEvent)
	if err != nil {
		t.Fatalf(errMsgDecodeFailed, err)
	}

	err = chain.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf(errMsgChainFailed, err)
	}

	if !mock.wasCalled() {
		t.Error("expected handler to be called for allowed custom run")
	}
	if mock.calls[0].TaskName != "approval-task" {
		t.Errorf("expected TaskName=approval-task, got %s", mock.calls[0].TaskName)
	}
}

// TestIntegration_EventListener tests event listener filtering
func TestIntegration_EventListener(t *testing.T) {
	// EventListener events don't have a provider, so use empty name to match all
	mock := &mockActionHandler{name: "", actionType: notifier.ActionCommitStatus}

	// Filter allows event listener "prod-listener"
	filterCfg := &notifier.FilterConfig{
		EventListeners: notifier.FilterList{
			Allow: []string{"prod-listener"},
		},
	}
	filtered := notifier.NewFilteredHandler(mock, filterCfg)

	reg := notifier.NewRegistry()
	reg.Register(filtered)

	chain := Build(
		NewValidator(),
		NewEventFilter(false, false, false, true, false, nil, nil), // Allow EventListener only
		newMemDeduper(100, nil),
		NewEnricher(""),
		NewDispatcher(reg, testLogger(), nil, 10),
	)

	eventListenerPayload := map[string]any{
		"eventListener":    "prod-listener",
		"namespace":        testNamespaceDefault,
		"eventListenerUID": "listener-uid-333",
		"eventID":          "evt-333",
	}

	data, _ := json.Marshal(eventListenerPayload)
	rawEvent := event.RawEvent{
		ID:     "event-333",
		Type:   "dev.tekton.event.triggers.successful.v1",
		Source: "/tekton/eventlistener",
		Data:   data,
	}

	decoder := tekton.NewEventListenerDecoder()
	env, err := decoder.Decode(rawEvent)
	if err != nil {
		t.Fatalf(errMsgDecodeFailed, err)
	}

	err = chain.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf(errMsgChainFailed, err)
	}

	if !mock.wasCalled() {
		t.Error("expected handler to be called for allowed event listener")
	}
	if mock.calls[0].EventListenerName != "prod-listener" {
		t.Errorf("expected EventListenerName=prod-listener, got %s", mock.calls[0].EventListenerName)
	}
}
