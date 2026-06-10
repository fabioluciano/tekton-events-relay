package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

type stubHandler struct {
	BaseHandler
	err error
}

func (s *stubHandler) Handle(_ context.Context, env *event.Envelope) error {
	if s.err != nil {
		return s.err
	}
	return s.Next(context.Background(), env)
}

func counterVecValue(cv *prometheus.CounterVec, labels ...string) float64 {
	m := &dto.Metric{}
	_ = cv.WithLabelValues(labels...).Write(m)
	return m.GetCounter().GetValue()
}

func histogramCount(obs prometheus.Observer) uint64 {
	m := &dto.Metric{}
	_ = obs.(prometheus.Metric).Write(m)
	return m.GetHistogram().GetSampleCount()
}

func TestWithMetrics_Success(t *testing.T) {
	duration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "test_duration_success",
	})
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_counter_success",
	}, []string{"step", testKeyStatus})

	inner := &stubHandler{}
	wrapped := WithMetrics(inner, duration, counter, "validate")

	env := &event.Envelope{CloudEventID: "test-1"}
	err := wrapped.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val := counterVecValue(counter, "validate", "success")
	if val != 1 {
		t.Errorf("expected success counter to be 1, got %f", val)
	}

	count := histogramCount(duration)
	if count != 1 {
		t.Errorf("expected histogram count to be 1, got %d", count)
	}
}

func TestWithMetrics_Error(t *testing.T) {
	duration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "test_duration_error",
	})
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_counter_error",
	}, []string{"step", testKeyStatus})

	inner := &stubHandler{err: errors.New("validation failed")}
	wrapped := WithMetrics(inner, duration, counter, "validate")

	env := &event.Envelope{CloudEventID: "test-2"}
	err := wrapped.Handle(context.Background(), env)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	val := counterVecValue(counter, "validate", "error")
	if val != 1 {
		t.Errorf("expected error counter to be 1, got %f", val)
	}
}

func TestWithMetrics_SetNext(t *testing.T) {
	duration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "test_duration_setnext",
	})
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_counter_setnext",
	}, []string{"step", testKeyStatus})

	inner := &stubHandler{}
	terminal := &stubHandler{}
	wrapped := WithMetrics(inner, duration, counter, "enrich")

	wrapped.SetNext(terminal)

	env := &event.Envelope{CloudEventID: "test-3"}
	err := wrapped.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
