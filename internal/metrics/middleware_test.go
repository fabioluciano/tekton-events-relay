package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func counterVecValue(cv *prometheus.CounterVec, labels ...string) float64 {
	m := &dto.Metric{}
	_ = cv.WithLabelValues(labels...).Write(m)
	return m.GetCounter().GetValue()
}

func TestNewCollectors(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewCollectors(reg)

	if c.EventsReceived == nil {
		t.Fatal("EventsReceived is nil")
	}
	if c.EventsProcessed == nil {
		t.Fatal("EventsProcessed is nil")
	}
	if c.HandlerDuration == nil {
		t.Fatal("HandlerDuration is nil")
	}
	if c.DeduperHits == nil {
		t.Fatal("DeduperHits is nil")
	}
	if c.PipelineErrors == nil {
		t.Fatal("PipelineErrors is nil")
	}
	if c.EventsFiltered == nil {
		t.Fatal("EventsFiltered is nil")
	}
	if c.ChainDuration == nil {
		t.Fatal("ChainDuration is nil")
	}
	if c.DedupeCacheSize == nil {
		t.Fatal("DedupeCacheSize is nil")
	}
	if c.HandlersRegistered == nil {
		t.Fatal("HandlersRegistered is nil")
	}
	if c.ErrorsPermanent == nil {
		t.Fatal("ErrorsPermanent is nil")
	}
	if c.EventsBackpressure == nil {
		t.Fatal("EventsBackpressure is nil")
	}
}

func TestHTTPMiddlewareWithCollectors(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewCollectors(reg)

	handler := HTTPMiddlewareWithCollectors(c)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Ce-Type", "dev.tekton.event.taskrun.started.v1")
	req.Header.Set("Ce-Source", "tekton-pipelines-controller")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	val := counterVecValue(c.EventsReceived, "dev.tekton.event.taskrun.started.v1", "tekton-pipelines-controller")
	if val != 1 {
		t.Errorf("expected EventsReceived to be 1, got %f", val)
	}
}
