package accumulator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/store"
)

func taskEvent(name string, state domain.State) *domain.Event {
	return &domain.Event{
		Resource: domain.ResourceTaskRun,
		RunName:  name,
		State:    state,
	}
}

func TestStoreBuffer_RoundTrip(t *testing.T) {
	st, err := store.New(config.StoreConfig{TTL: time.Minute}, store.Options{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	buf := NewStoreBuffer(st.RunBuffer(), st.Backend(), nil, nil)

	buf.Add("uid-1", taskEvent("build", domain.StateSuccess))
	buf.Add("uid-1", taskEvent("test", domain.StateFailure))
	// Non-TaskRun events must be ignored, like LRUBuffer does.
	buf.Add("uid-1", &domain.Event{Resource: domain.ResourcePipelineRun, RunName: "run"})

	state, found := buf.Flush("uid-1")
	if !found {
		t.Fatal("expected state for uid-1")
	}
	if len(state.Tasks) != 2 {
		t.Fatalf("len(Tasks) = %d, want 2", len(state.Tasks))
	}
	if state.Tasks["build"].State != domain.StateSuccess {
		t.Errorf("build state = %q, want success", state.Tasks["build"].State)
	}

	if _, found := buf.Flush("uid-1"); found {
		t.Error("second Flush should report not found")
	}
}

type failingRunBuffer struct{}

func (failingRunBuffer) Add(context.Context, string, string, *domain.Event) error {
	return errors.New("backend down")
}

func (failingRunBuffer) Flush(context.Context, string) (map[string]*domain.Event, bool, error) {
	return nil, false, errors.New("backend down")
}

func TestStoreBuffer_FailsOpenOnError(t *testing.T) {
	collectors := metrics.NewCollectors(prometheus.NewRegistry())
	buf := NewStoreBuffer(failingRunBuffer{}, "valkey", collectors, nil)

	buf.Add("uid-1", taskEvent("build", domain.StateSuccess)) // must not panic
	if _, found := buf.Flush("uid-1"); found {
		t.Error("Flush on failing backend should report not found")
	}

	if got := testutil.ToFloat64(collectors.StoreErrors.WithLabelValues("valkey", "add")); got != 1 {
		t.Errorf("store_errors{add} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(collectors.StoreErrors.WithLabelValues("valkey", "flush")); got != 1 {
		t.Errorf("store_errors{flush} = %v, want 1", got)
	}
}
