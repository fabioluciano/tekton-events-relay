package pipeline

import (
	"context"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// terminal counts how many times Handle was called - terminal handler for tests.
type terminal struct {
	BaseHandler
	count int
}

func (t *terminal) Handle(ctx context.Context, env *event.Envelope) error {
	t.count++
	return t.Next(ctx, env)
}

func sample(id string) *event.Envelope {
	return &event.Envelope{
		CloudEventID:   id,
		CloudEventType: "dev.tekton.event.pipelinerun.successful.v1",
		Report: domain.Event{
			Provider:  "github",
			Resource:  domain.ResourcePipelineRun,
			CommitSHA: "abc",
			RunName:   "run-1",
			RunID:     "550e8400-e29b-41d4-a716-446655440000",
			State:     domain.StateSuccess,
		},
	}
}

func TestValidator_OK(t *testing.T) {
	v := NewValidator()
	term := &terminal{}
	Build(v, term)
	if err := v.Handle(context.Background(), sample("evt-1")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if term.count != 1 {
		t.Errorf("terminal called %d times, want 1", term.count)
	}
}

func TestValidator_Rejects(t *testing.T) {
	v := NewValidator()
	env := sample("evt-1")
	env.CloudEventID = ""
	if err := v.Handle(context.Background(), env); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidator_RejectsNilEnvelope(t *testing.T) {
	v := NewValidator()
	if err := v.Handle(context.Background(), nil); err == nil {
		t.Fatal("expected validation error for nil envelope")
	}
}

func TestValidator_RejectsMissingProvider(t *testing.T) {
	v := NewValidator()
	env := sample("evt-1")
	env.Report.Provider = ""
	if err := v.Handle(context.Background(), env); err == nil {
		t.Fatal("expected validation error for missing Provider")
	}
}

func TestValidator_RejectsMissingCommitSHA(t *testing.T) {
	v := NewValidator()
	env := sample("evt-1")
	env.Report.CommitSHA = ""
	if err := v.Handle(context.Background(), env); err == nil {
		t.Fatal("expected validation error for missing CommitSHA")
	}
}

func TestValidator_RejectsMissingRunName(t *testing.T) {
	v := NewValidator()
	env := sample("evt-1")
	env.Report.RunName = ""
	if err := v.Handle(context.Background(), env); err == nil {
		t.Fatal("expected validation error for missing RunName")
	}
}

func TestEventFilter_DropsTaskRunWhenDisabled(t *testing.T) {
	f := NewEventFilter(false, true, false)
	term := &terminal{}
	Build(f, term)

	env := sample("evt-1")
	env.Report.Resource = domain.ResourceTaskRun
	if err := f.Handle(context.Background(), env); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if term.count != 0 {
		t.Errorf("terminal called %d times, want 0 (filtered)", term.count)
	}
}

func TestEventFilter_DropsPipelineRunWhenDisabled(t *testing.T) {
	f := NewEventFilter(true, false, false)
	term := &terminal{}
	Build(f, term)

	env := sample("evt-1")
	env.Report.Resource = domain.ResourcePipelineRun
	if err := f.Handle(context.Background(), env); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if term.count != 0 {
		t.Errorf("terminal called %d times, want 0 (filtered)", term.count)
	}
}

func TestEventFilter_PassesTaskRunWhenEnabled(t *testing.T) {
	f := NewEventFilter(true, false, false)
	term := &terminal{}
	Build(f, term)

	env := sample("evt-1")
	env.Report.Resource = domain.ResourceTaskRun
	if err := f.Handle(context.Background(), env); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if term.count != 1 {
		t.Errorf("terminal called %d times, want 1 (passed through)", term.count)
	}
}

func TestEventFilter_DropsUnknown(t *testing.T) {
	f := NewEventFilter(true, true, true)
	term := &terminal{}
	Build(f, term)

	env := sample("evt-1")
	env.CloudEventType = "dev.tekton.event.pipelinerun.unknown.v1"
	if err := f.Handle(context.Background(), env); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if term.count != 0 {
		t.Errorf("expected unknown to be dropped")
	}
}

func TestDeduper_RejectsRepeats(t *testing.T) {
	d := NewDeduper(100)
	term := &terminal{}
	Build(d, term)

	_ = d.Handle(context.Background(), sample("evt-1"))
	_ = d.Handle(context.Background(), sample("evt-1"))
	_ = d.Handle(context.Background(), sample("evt-2"))

	if term.count != 2 {
		t.Errorf("terminal count = %d, want 2 (1 dedup'd)", term.count)
	}
}

func TestDeduper_LRUEviction(t *testing.T) {
	d := NewDeduper(2) // capacity=2
	term := &terminal{}
	Build(d, term)

	// Send 3 unique events: capacity+1 triggers eviction
	_ = d.Handle(context.Background(), sample("evt-1")) // passes, cache: [1]
	_ = d.Handle(context.Background(), sample("evt-2")) // passes, cache: [2, 1]
	_ = d.Handle(context.Background(), sample("evt-3")) // passes, cache: [3, 2], evicts evt-1 (oldest)

	if term.count != 3 {
		t.Errorf("terminal count = %d, want 3 (all passed)", term.count)
	}

	// Re-send evt-1: should pass again (was evicted)
	_ = d.Handle(context.Background(), sample("evt-1")) // passes, cache: [1, 3], evicts evt-2
	if term.count != 4 {
		t.Errorf("terminal count after evt-1 re-send = %d, want 4 (evt-1 re-admitted)", term.count)
	}

	// Re-send evt-3: should be dropped (still in cache)
	_ = d.Handle(context.Background(), sample("evt-3")) // cached, MoveToFront
	if term.count != 4 {
		t.Errorf("terminal count after evt-3 re-send = %d, want 4 (evt-3 still cached)", term.count)
	}

	// Re-send evt-2: should pass again (was evicted when evt-1 re-entered)
	_ = d.Handle(context.Background(), sample("evt-2")) // passes, was evicted
	if term.count != 5 {
		t.Errorf("terminal count after evt-2 re-send = %d, want 5 (evt-2 was evicted)", term.count)
	}
}

func TestDeduper_DefaultCapacity(t *testing.T) {
	d := NewDeduper(0) // capacity <= 0 defaults to 10000
	term := &terminal{}
	Build(d, term)

	// Send 2 events
	_ = d.Handle(context.Background(), sample("evt-1"))
	_ = d.Handle(context.Background(), sample("evt-1")) // duplicate

	if term.count != 1 {
		t.Errorf("terminal count = %d, want 1 (duplicate rejected)", term.count)
	}
}

func TestDeduper_NegativeCapacity(t *testing.T) {
	d := NewDeduper(-5) // negative capacity defaults to 10000
	term := &terminal{}
	Build(d, term)

	_ = d.Handle(context.Background(), sample("evt-1"))
	_ = d.Handle(context.Background(), sample("evt-1"))

	if term.count != 1 {
		t.Errorf("terminal count = %d, want 1", term.count)
	}
}

func TestEnricher_AddsDashboardURL(t *testing.T) {
	e := NewEnricher("https://tekton.company.com")
	term := &terminal{}
	Build(e, term)

	env := sample("evt-1")
	env.Report.Namespace = "ci"
	if err := e.Handle(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	want := "https://tekton.company.com/#/namespaces/ci/pipelineruns/run-1"
	if env.Report.TargetURL != want {
		t.Errorf("TargetURL = %q, want %q", env.Report.TargetURL, want)
	}
}

func TestEnricher_KeepsExistingTargetURL(t *testing.T) {
	e := NewEnricher("https://tekton.company.com")
	env := sample("evt-1")
	env.Report.TargetURL = "https://my-custom.url/x"
	_ = e.Handle(context.Background(), env)
	if env.Report.TargetURL != "https://my-custom.url/x" {
		t.Errorf("custom URL was overwritten")
	}
}

func TestChain_OrdersHandlers(t *testing.T) {
	v := NewValidator()
	d := NewDeduper(100)
	term := &terminal{}
	first := Build(v, d, term)

	if err := first.Handle(context.Background(), sample("evt-1")); err != nil {
		t.Fatal(err)
	}
	if term.count != 1 {
		t.Errorf("got %d, want 1", term.count)
	}
}

func TestChain_BuildEmptyHandlers(t *testing.T) {
	result := Build()
	if result != nil {
		t.Errorf("Build() with no handlers = %v, want nil", result)
	}
}
