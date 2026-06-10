package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
)

type failingDedupeStore struct{}

func (failingDedupeStore) FirstSeen(context.Context, string) (bool, error) {
	return false, errors.New("backend down")
}

func TestDeduper_FailsOpenOnStoreError(t *testing.T) {
	collectors := metrics.NewCollectors(prometheus.NewRegistry())
	d := NewDeduperWithStore(failingDedupeStore{}, "valkey", collectors, nil)
	term := &terminal{}
	Build(d, term)

	if err := d.Handle(context.Background(), sample(testEventID1)); err != nil {
		t.Fatalf(testUnexpectedErr, err)
	}
	if term.count != 1 {
		t.Errorf("terminal called %d times, want 1 (fail open)", term.count)
	}
	if got := testutil.ToFloat64(collectors.StoreErrors.WithLabelValues("valkey", "dedupe")); got != 1 {
		t.Errorf("store_errors_total = %v, want 1", got)
	}
}
