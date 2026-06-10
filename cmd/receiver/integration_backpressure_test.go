//go:build integration
// +build integration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	relayerrors "github.com/fabioluciano/tekton-events-relay/internal/errors"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/event/tekton"
	httpx "github.com/fabioluciano/tekton-events-relay/internal/http"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
)

// retryableChain is a pipeline.Handler that always returns a RetryableError.
type retryableChain struct {
	pipeline.BaseHandler
}

func (r *retryableChain) Handle(_ context.Context, _ *event.Envelope) error {
	return relayerrors.NewRetryable(errors.New("downstream timeout"), "rate_limit")
}

func (r *retryableChain) SetNext(_ pipeline.Handler) {}

// permanentErrorChain is a pipeline.Handler that always returns a permanent error.
type permanentErrorChain struct {
	pipeline.BaseHandler
}

func (p *permanentErrorChain) Handle(_ context.Context, _ *event.Envelope) error {
	return errors.New("permanent chain error")
}

func (p *permanentErrorChain) SetNext(_ pipeline.Handler) {}

// successChain is a pipeline.Handler that always succeeds.
type successChain struct {
	pipeline.BaseHandler
}

func (s *successChain) Handle(_ context.Context, _ *event.Envelope) error {
	return nil
}

func (s *successChain) SetNext(_ pipeline.Handler) {}

// stubDecoder implements event.Decoder for testing.
type stubDecoder struct{}

func (d *stubDecoder) Name() string            { return "stub-decoder" }
func (d *stubDecoder) CanHandle(t string) bool { return strings.HasPrefix(t, "dev.tekton.event.") }
func (d *stubDecoder) Decode(raw event.RawEvent) (*event.Envelope, error) {
	return &event.Envelope{
		CloudEventID:   raw.ID,
		CloudEventType: raw.Type,
		Source:         raw.Source,
	}, nil
}

// TestBackpressure503_Integration tests end-to-end flow:
// 1. Mock chain that returns RetryableError
// 2. CloudEventsHandler should return 503
// 3. Metrics EventsBackpressure should increment
func TestBackpressure503_Integration(t *testing.T) {
	logger := zap.NewNop()

	// Setup metrics
	promReg := prometheus.NewRegistry()
	collectors := metrics.NewCollectors(promReg)

	// Setup decoders
	decoders := event.NewRegistry()
	decoders.Register(tekton.NewTaskRunDecoder())

	// Setup chain that returns RetryableError
	chain := &retryableChain{}

	// Create handler
	handler := httpx.CloudEventsHandler(decoders, chain, logger, collectors, false, nil)

	// Create TaskRun CloudEvent payload
	taskRunPayload := map[string]any{
		"taskRun": map[string]any{
			"metadata": map[string]any{
				"name":      "test-taskrun-123",
				"namespace": "default",
				"uid":       "task-uid-123",
				"annotations": map[string]any{
					"tekton.dev/tekton-events-relay.scm.provider":   "github",
					"tekton.dev/tekton-events-relay.scm.repo-owner": "myorg",
					"tekton.dev/tekton-events-relay.scm.repo-name":  "myrepo",
					"tekton.dev/tekton-events-relay.scm.commit-sha": "abc123",
				},
				"labels": map[string]any{
					"tekton.dev/task": "build",
				},
			},
			"status": map[string]any{},
		},
	}

	data, err := json.Marshal(taskRunPayload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	// Create HTTP request with CloudEvent headers
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ce-Id", "event-123")
	req.Header.Set("Ce-Type", "dev.tekton.event.taskrun.successful.v1")
	req.Header.Set("Ce-Source", "/tekton/controller")
	req.Header.Set("Ce-Specversion", "1.0")

	// Record response
	rr := httptest.NewRecorder()

	// Execute handler
	handler.ServeHTTP(rr, req)

	// Assert: status 503
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rr.Code)
	}

	// Assert: EventsBackpressure metric incremented
	metricFamilies, err := promReg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var backpressureCount float64
	for _, mf := range metricFamilies {
		if mf.GetName() == "tekton_events_relay_events_backpressure_total" {
			for _, m := range mf.GetMetric() {
				backpressureCount = m.GetCounter().GetValue()
			}
		}
	}

	if backpressureCount != 1 {
		t.Errorf("expected EventsBackpressure=1, got %v", backpressureCount)
	}
}

// TestPermanentError200_Integration tests:
// 1. Mock chain that returns normal error
// 2. CloudEventsHandler should return 200 (acknowledged)
// 3. Metrics ErrorsPermanent should increment
func TestPermanentError200_Integration(t *testing.T) {
	logger := zap.NewNop()

	// Setup metrics
	promReg := prometheus.NewRegistry()
	collectors := metrics.NewCollectors(promReg)

	// Setup decoders
	decoders := event.NewRegistry()
	decoders.Register(&stubDecoder{})

	// Setup chain that returns permanent error
	chain := &permanentErrorChain{}

	// Create handler
	handler := httpx.CloudEventsHandler(decoders, chain, logger, collectors, false, nil)

	// Create TaskRun CloudEvent payload
	taskRunPayload := map[string]any{
		"taskRun": map[string]any{
			"metadata": map[string]any{
				"name":      "test-taskrun-456",
				"namespace": "default",
				"uid":       "task-uid-456",
				"annotations": map[string]any{
					"tekton.dev/tekton-events-relay.scm.provider":   "github",
					"tekton.dev/tekton-events-relay.scm.repo-owner": "myorg",
					"tekton.dev/tekton-events-relay.scm.repo-name":  "myrepo",
					"tekton.dev/tekton-events-relay.scm.commit-sha": "abc123",
				},
				"labels": map[string]any{
					"tekton.dev/task": "test",
				},
			},
			"status": map[string]any{},
		},
	}

	data, err := json.Marshal(taskRunPayload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	// Create HTTP request with CloudEvent headers
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ce-Id", "event-456")
	req.Header.Set("Ce-Type", "dev.tekton.event.taskrun.successful.v1")
	req.Header.Set("Ce-Source", "/tekton/controller")
	req.Header.Set("Ce-Specversion", "1.0")

	// Record response
	rr := httptest.NewRecorder()

	// Execute handler
	handler.ServeHTTP(rr, req)

	// Assert: status 200 (acknowledged)
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Assert: ErrorsPermanent metric incremented
	metricFamilies, err := promReg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var permanentErrorCount float64
	for _, mf := range metricFamilies {
		if mf.GetName() == "tekton_events_relay_errors_permanent_total" {
			for _, m := range mf.GetMetric() {
				// Check for chain_error label
				for _, lp := range m.GetLabel() {
					if lp.GetName() == "reason" && lp.GetValue() == "chain_error" {
						permanentErrorCount = m.GetCounter().GetValue()
					}
				}
			}
		}
	}

	if permanentErrorCount != 1 {
		t.Errorf("expected ErrorsPermanent=1, got %v", permanentErrorCount)
	}
}

// TestSuccessful200_Integration tests:
// 1. Mock chain that succeeds
// 2. CloudEventsHandler should return 200
// 3. No error metrics should increment
func TestSuccessful200_Integration(t *testing.T) {
	logger := zap.NewNop()

	// Setup metrics
	promReg := prometheus.NewRegistry()
	collectors := metrics.NewCollectors(promReg)

	// Setup decoders
	decoders := event.NewRegistry()
	decoders.Register(&stubDecoder{})

	// Setup chain that succeeds
	chain := &successChain{}

	// Create handler
	handler := httpx.CloudEventsHandler(decoders, chain, logger, collectors, false, nil)

	// Create TaskRun CloudEvent payload
	taskRunPayload := map[string]any{
		"taskRun": map[string]any{
			"metadata": map[string]any{
				"name":      "test-taskrun-789",
				"namespace": "default",
				"uid":       "task-uid-789",
				"annotations": map[string]any{
					"tekton.dev/tekton-events-relay.scm.provider":   "github",
					"tekton.dev/tekton-events-relay.scm.repo-owner": "myorg",
					"tekton.dev/tekton-events-relay.scm.repo-name":  "myrepo",
					"tekton.dev/tekton-events-relay.scm.commit-sha": "abc123",
				},
				"labels": map[string]any{
					"tekton.dev/task": "deploy",
				},
			},
			"status": map[string]any{},
		},
	}

	data, err := json.Marshal(taskRunPayload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	// Create HTTP request with CloudEvent headers
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ce-Id", "event-789")
	req.Header.Set("Ce-Type", "dev.tekton.event.taskrun.successful.v1")
	req.Header.Set("Ce-Source", "/tekton/controller")
	req.Header.Set("Ce-Specversion", "1.0")

	// Record response
	rr := httptest.NewRecorder()

	// Execute handler
	handler.ServeHTTP(rr, req)

	// Assert: status 200
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Assert: no error metrics incremented
	metricFamilies, err := promReg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	for _, mf := range metricFamilies {
		if mf.GetName() == "tekton_events_relay_events_backpressure_total" {
			for _, m := range mf.GetMetric() {
				count := m.GetCounter().GetValue()
				if count != 0 {
					t.Errorf("expected EventsBackpressure=0, got %v", count)
				}
			}
		}
		if mf.GetName() == "tekton_events_relay_errors_permanent_total" {
			for _, m := range mf.GetMetric() {
				count := m.GetCounter().GetValue()
				if count != 0 {
					t.Errorf("expected ErrorsPermanent=0, got %v", count)
				}
			}
		}
	}
}
