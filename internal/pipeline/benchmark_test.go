package pipeline

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/store"
)

// benchServer is a pre-started httptest server available to all benchmarks.
var benchServer *httptest.Server

// benchSpyHandler satisfies the current ActionHandler interface (includes Close).
type benchSpyHandler struct {
	name       string
	actionType notifier.ActionType
	callCount  int
}

func (h *benchSpyHandler) Name() string              { return h.name }
func (h *benchSpyHandler) Provider() string          { return h.name }
func (h *benchSpyHandler) Type() notifier.ActionType { return h.actionType }
func (h *benchSpyHandler) Handle(_ context.Context, _ domain.Event) error {
	h.callCount++
	return nil
}
func (h *benchSpyHandler) Close() error { return nil }

func TestMain(m *testing.M) {
	benchServer = httptest.NewServer(nil)
	code := m.Run()
	benchServer.Close()
	os.Exit(code)
}

// BenchmarkDispatch measures fan-out latency with 100 spy handlers
// processing a single event through the Dispatcher.
func BenchmarkDispatch(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	const numHandlers = 100

	registry := notifier.NewRegistry()
	for i := 0; i < numHandlers; i++ {
		registry.Register(&benchSpyHandler{
			name:       fmt.Sprintf("handler-%d", i),
			actionType: notifier.ActionCommitStatus,
		})
	}

	dispatcher := NewDispatcher(registry, testLogger(), nil, numHandlers)
	dispatcher.SetNext(&testHandler{fn: func(_ context.Context, _ *event.Envelope) error {
		return nil
	}})

	env := &event.Envelope{
		CloudEventID: "bench-dispatch-id",
		Report: domain.Event{
			Provider:  testProviderGitHub,
			Resource:  domain.ResourcePipelineRun,
			CommitSHA: "abc123",
			RunName:   "bench-run",
			RunID:     "bench-uid",
			State:     domain.StateSuccess,
		},
	}

	b.ResetTimer()
	for b.Loop() {
		_ = dispatcher.Handle(context.Background(), env)
	}
}

// BenchmarkCELEval benchmarks evaluating 100 distinct pre-compiled CEL
// expressions against a single domain.Event.
func BenchmarkCELEval(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	expressions := make([]*cel.Program, 100)
	for i := range expressions {
		expr := fmt.Sprintf(`event.State == "success" && event.Namespace == "ns-%d"`, i)
		prog, err := cel.Compile(expr)
		if err != nil {
			b.Fatalf("failed to compile CEL expression %d: %v", i, err)
		}
		expressions[i] = prog
	}

	e := domain.Event{
		Resource:  domain.ResourcePipelineRun,
		State:     domain.StateSuccess,
		Namespace: "ns-50",
		RunName:   "bench-run",
		Provider:  testProviderGitHub,
		CommitSHA: "abc123",
	}

	b.ResetTimer()
	for b.Loop() {
		for _, prog := range expressions {
			_, _ = prog.Eval(e)
		}
	}
}

// BenchmarkDedup benchmarks 1000 unique IDs through the in-memory
// DedupeStore.FirstSeen path.
func BenchmarkDedup(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	mem, err := store.New(config.StoreConfig{Backend: store.BackendMemory},
		store.Options{DedupeCapacity: 100000})
	if err != nil {
		b.Fatalf("failed to create memory store: %v", err)
	}
	dedup := mem.Dedupe()

	const uniqueIDs = 1000
	ids := make([]string, uniqueIDs)
	for i := range ids {
		ids[i] = fmt.Sprintf("bench-dedup-id-%d", i)
	}

	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		for _, id := range ids {
			_, _ = dedup.FirstSeen(ctx, id)
		}
	}
}
