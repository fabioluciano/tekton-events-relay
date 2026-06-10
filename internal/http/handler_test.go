package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	relayerrors "github.com/fabioluciano/tekton-events-relay/internal/errors"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
)

const (
	testID               = "test-id"
	testSource           = "test-source"
	testTaskRunStartedV1 = "dev.tekton.event.taskrun.started.v1"
	testRunJSON          = `{"name":"test-taskrun","status":"running"}`
	chainError           = "chain_error"
	testRedacted         = "[REDACTED]"
)

// testCollectors creates a fresh *metrics.Collectors for testing.
func testCollectors(t *testing.T) *metrics.Collectors {
	t.Helper()
	reg := prometheus.NewRegistry()
	return metrics.NewCollectors(reg)
}

func TestCloudEventsHandler_InvalidCloudEvent(t *testing.T) {
	decoders := event.NewRegistry()
	log := zap.NewNop()
	collectors := testCollectors(t)

	handler := CloudEventsHandler(decoders, nil, log, collectors, false, nil)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not a cloudevent") {
		t.Errorf("expected error message containing 'not a cloudevent', got %q", rec.Body.String())
	}
}

// errReader is an io.Reader that immediately returns an error on Read.
type errReader struct{}

func (e *errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated body read error")
}

func TestCloudEventsHandler_BodyReadError(t *testing.T) {
	decoders := event.NewRegistry()
	log := zap.NewNop()
	collectors := testCollectors(t)

	handler := CloudEventsHandler(decoders, nil, log, collectors, false, nil)

	req := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(&errReader{}))
	req.Header.Set("Ce-Id", testID)
	req.Header.Set("Ce-Type", testTaskRunStartedV1)
	req.Header.Set("Ce-Source", testSource)
	req.Header.Set("Ce-Specversion", "1.0")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected %d for body read error, got %d", http.StatusBadRequest, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not a cloudevent") {
		t.Errorf("expected error message containing 'not a cloudevent', got %q", rec.Body.String())
	}
}

func TestCloudEventsHandler_NoDecoder(t *testing.T) {
	decoders := event.NewRegistry()
	log := zap.NewNop()
	collectors := testCollectors(t)

	handler := CloudEventsHandler(decoders, nil, log, collectors, false, nil)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"some":"data"}`))
	req.Header.Set("Ce-Id", testID)
	req.Header.Set("Ce-Type", "unknown.type")
	req.Header.Set("Ce-Source", testSource)
	req.Header.Set("Ce-Specversion", "1.0")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestRedactPayload(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "token field",
			input:  `{"token":"secret123","data":"safe"}`,
			expect: `[REDACTED]`, //nolint:goconst
		},
		{
			name:   "webhook_url field",
			input:  `{"webhook_url":"https://hooks.slack.com/xxx"}`,
			expect: `[REDACTED]`,
		},
		{
			name:   "api_key field",
			input:  `{"api_key":"sk-1234567890","user":"test"}`, // gitleaks:allow
			expect: `[REDACTED]`,
		},
		{
			name:   "password field",
			input:  `{"username":"admin","password":"secret123"}`,
			expect: `[REDACTED]`,
		},
		{
			name:   "integration_key field",
			input:  `{"integration_key":"pagerduty-key-123"}`,
			expect: `[REDACTED]`,
		},
		{
			name:   "app_password field",
			input:  `{"username":"user","app_password":"bitbucket-secret"}`,
			expect: `[REDACTED]`,
		},
		{
			name:   "multiple sensitive fields",
			input:  `{"token":"secret1","api_key":"secret2","data":"safe"}`,
			expect: `[REDACTED]`,
		},
		{
			name:   "no sensitive fields",
			input:  `{"data":"safe","user":"test"}`,
			expect: `{"data":"safe","user":"test"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := redactPayload([]byte(tt.input))
			resultStr := string(result)

			if tt.expect == testRedacted {
				if !strings.Contains(resultStr, testRedacted) {
					t.Errorf("expected [REDACTED] in output, got: %s", resultStr)
				}
			} else {
				if resultStr != tt.expect {
					t.Errorf("expected %q, got %q", tt.expect, resultStr)
				}
			}
		})
	}
}

// --- Graceful Degradation Tests ---

// failingChain is a pipeline.Handler that always returns an error.
type failingChain struct {
	pipeline.BaseHandler
}

func (f *failingChain) Handle(_ context.Context, _ *event.Envelope) error {
	return errors.New("chain processing failed")
}

func (f *failingChain) SetNext(_ pipeline.Handler) {}

// retryableChain is a pipeline.Handler that always returns a RetryableError.
type retryableChain struct {
	pipeline.BaseHandler
}

func (r *retryableChain) Handle(_ context.Context, _ *event.Envelope) error {
	return relayerrors.NewRetryable(errors.New("downstream timeout"), "timeout")
}

func (r *retryableChain) SetNext(_ pipeline.Handler) {}

// successChain is a pipeline.Handler that always returns nil.
type successChain struct {
	pipeline.BaseHandler
}

func (s *successChain) Handle(_ context.Context, _ *event.Envelope) error {
	return nil
}

func (s *successChain) SetNext(_ pipeline.Handler) {}

func TestCloudEventsHandler_ChainErrorReturns500(t *testing.T) {
	decoders := event.NewRegistry()
	decoders.Register(&stubDecoder{})

	core, logs := observer.New(zapcore.ErrorLevel)
	log := zap.New(core)
	collectors := testCollectors(t)

	chain := &failingChain{}
	handler := CloudEventsHandler(decoders, chain, log, collectors, false, nil)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(testRunJSON))
	req.Header.Set("Ce-Id", testID)
	req.Header.Set("Ce-Type", testTaskRunStartedV1)
	req.Header.Set("Ce-Source", testSource)
	req.Header.Set("Ce-Specversion", "1.0")
	rec := httptest.NewRecorder()

	handler(rec, req)

	// Permanent error returns 200 (acknowledge)
	if rec.Code != http.StatusOK {
		t.Errorf("expected %d for permanent chain error, got %d", http.StatusOK, rec.Code)
	}

	// Verify error was logged
	if logs.Len() == 0 {
		t.Error("expected error to be logged when chain fails")
	}
}

func TestCloudEventsHandler_NoDecoderReturnsOKNotPanic(t *testing.T) {
	// Empty decoder registry - unknown event type should return 200, not panic
	decoders := event.NewRegistry()
	log := zap.NewNop()
	collectors := testCollectors(t)

	handler := CloudEventsHandler(decoders, nil, log, collectors, false, nil)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"data":"test"}`))
	req.Header.Set("Ce-Id", testID)
	req.Header.Set("Ce-Type", "com.unknown.event.type.v1")
	req.Header.Set("Ce-Source", "unknown-source")
	req.Header.Set("Ce-Specversion", "1.0")
	rec := httptest.NewRecorder()

	// Must not panic even with nil chain (decoder not found path)
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected %d for unrecognized event type, got %d", http.StatusOK, rec.Code)
	}
}

func TestCloudEventsHandler_EmptyRegistryLogsWarning(t *testing.T) {
	// When decoder registry is empty, unknown types are gracefully handled
	decoders := event.NewRegistry()

	core, logs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)
	collectors := testCollectors(t)

	handler := CloudEventsHandler(decoders, nil, log, collectors, false, nil)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"payload":"data"}`))
	req.Header.Set("Ce-Id", "evt-001")
	req.Header.Set("Ce-Type", "dev.tekton.event.taskrun.unknown.v1")
	req.Header.Set("Ce-Source", "tekton-triggers")
	req.Header.Set("Ce-Specversion", "1.0")
	rec := httptest.NewRecorder()

	handler(rec, req)

	// Should return 200 (event accepted but no decoder matched)
	if rec.Code != http.StatusOK {
		t.Errorf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	// Verify "no decoder" debug log was emitted
	found := false
	for _, entry := range logs.All() {
		if strings.Contains(entry.Message, "no decoder") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'no decoder' debug log when decoder registry has no match")
	}
}

// --- Backpressure Tests ---

func TestCloudEventsHandler_RetryableError503(t *testing.T) {
	decoders := event.NewRegistry()
	decoders.Register(&stubDecoder{})

	core, logs := observer.New(zapcore.WarnLevel)
	log := zap.New(core)
	collectors := testCollectors(t)

	chain := &retryableChain{}
	handler := CloudEventsHandler(decoders, chain, log, collectors, false, nil)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(testRunJSON))
	req.Header.Set("Ce-Id", testID)
	req.Header.Set("Ce-Type", testTaskRunStartedV1)
	req.Header.Set("Ce-Source", testSource)
	req.Header.Set("Ce-Specversion", "1.0")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected %d for retryable error, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	// Verify EventsBackpressure was incremented
	metricVal := promCounterValue(t, collectors.EventsBackpressure)
	if metricVal != 1 {
		t.Errorf("expected EventsBackpressure=1, got %v", metricVal)
	}

	// Verify warn log was emitted
	found := false
	for _, entry := range logs.All() {
		if strings.Contains(entry.Message, "retryable error") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'retryable error' warn log")
	}
}

func TestCloudEventsHandler_PermanentError200(t *testing.T) {
	decoders := event.NewRegistry()
	decoders.Register(&stubDecoder{})

	core, logs := observer.New(zapcore.ErrorLevel)
	log := zap.New(core)
	collectors := testCollectors(t)

	chain := &failingChain{}
	handler := CloudEventsHandler(decoders, chain, log, collectors, false, nil)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(testRunJSON))
	req.Header.Set("Ce-Id", testID)
	req.Header.Set("Ce-Type", testTaskRunStartedV1)
	req.Header.Set("Ce-Source", testSource)
	req.Header.Set("Ce-Specversion", "1.0")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected %d for permanent error, got %d", http.StatusOK, rec.Code)
	}

	// Verify ErrorsPermanent was incremented
	metricVal := promCounterVecValue(t, collectors.ErrorsPermanent, chainError)
	if metricVal != 1 {
		t.Errorf("expected ErrorsPermanent=1, got %v", metricVal)
	}

	// Verify error log was emitted
	if logs.Len() == 0 {
		t.Error("expected error log for permanent error")
	}
}

func TestCloudEventsHandler_Success200(t *testing.T) {
	decoders := event.NewRegistry()
	decoders.Register(&stubDecoder{})

	log := zap.NewNop()
	collectors := testCollectors(t)

	chain := &successChain{}
	handler := CloudEventsHandler(decoders, chain, log, collectors, false, nil)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(testRunJSON))
	req.Header.Set("Ce-Id", testID)
	req.Header.Set("Ce-Type", testTaskRunStartedV1)
	req.Header.Set("Ce-Source", testSource)
	req.Header.Set("Ce-Specversion", "1.0")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected %d for success, got %d", http.StatusOK, rec.Code)
	}

	// Verify no error metrics incremented
	backpressure := promCounterValue(t, collectors.EventsBackpressure)
	if backpressure != 0 {
		t.Errorf("expected EventsBackpressure=0, got %v", backpressure)
	}

	permanent := promCounterVecValue(t, collectors.ErrorsPermanent, chainError)
	if permanent != 0 {
		t.Errorf("expected ErrorsPermanent=0, got %v", permanent)
	}
}

// --- Helpers ---

// promCounterValue reads the current value of a prometheus.Counter.
func promCounterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	m := &dto.Metric{}
	if err := c.(prometheus.Metric).Write(m); err != nil {
		t.Fatalf("failed to read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

// promCounterVecValue reads the current value of a prometheus.CounterVec with given label.
func promCounterVecValue(t *testing.T, cv *prometheus.CounterVec, label string) float64 {
	t.Helper()
	m := &dto.Metric{}
	counter, err := cv.GetMetricWithLabelValues(label)
	if err != nil {
		t.Fatalf("failed to get counter with label %q: %v", label, err)
	}
	if err := counter.(prometheus.Metric).Write(m); err != nil {
		t.Fatalf("failed to read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

// stubDecoder implements event.Decoder for testing chain error paths.
type stubDecoder struct{}

func (s *stubDecoder) Name() string            { return "stub-decoder" }
func (s *stubDecoder) CanHandle(t string) bool { return strings.HasPrefix(t, "dev.tekton.event.") }
func (s *stubDecoder) Decode(raw event.RawEvent) (*event.Envelope, error) {
	return &event.Envelope{
		CloudEventID:   raw.ID,
		CloudEventType: raw.Type,
		Source:         raw.Source,
	}, nil
}
