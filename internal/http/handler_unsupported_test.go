package http

import (
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/cehttp"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
)

func TestDecodeEvent_UnsupportedTypeCountsMetric(t *testing.T) {
	collectors := metrics.NewCollectors(prometheus.NewRegistry())
	decoders := event.NewRegistry() // no decoders registered
	rec := httptest.NewRecorder()

	ce := &cehttp.Event{
		ID:   "evt-1",
		Type: "dev.tekton.event.steprun.started.v1",
	}

	env, ok := decodeEvent(decoders, ce, zap.NewNop(), collectors, rec)
	if ok || env != nil {
		t.Fatalf("expected decode to fail for unsupported type")
	}
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200 (event acknowledged, not retried)", rec.Code)
	}

	got := testutil.ToFloat64(collectors.EventsUnsupportedType.WithLabelValues("dev.tekton.event.steprun.started.v1"))
	if got != 1 {
		t.Errorf("events_unsupported_type_total = %v, want 1", got)
	}
}

func TestDecodeEvent_NilCollectorsDoesNotPanic(t *testing.T) {
	decoders := event.NewRegistry()
	rec := httptest.NewRecorder()
	ce := &cehttp.Event{ID: "evt-2", Type: "unknown.type"}

	_, ok := decodeEvent(decoders, ce, zap.NewNop(), nil, rec)
	if ok {
		t.Fatal("expected decode to fail")
	}
}
