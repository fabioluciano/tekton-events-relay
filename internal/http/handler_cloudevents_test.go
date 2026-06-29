package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

const captureProvider = "github"

func TestCloudEventsHandler_PreservesBinaryModeBodyForDecoder(t *testing.T) {
	// Given: a binary-mode CloudEvent whose JSON body must reach the Tekton decoder unchanged.
	body := `{"pipelineRun":{"metadata":{"name":"build-123","annotations":{"token":"not-redacted-before-decode"}}}}`
	decoder := &capturingDecoder{eventType: testTaskRunStartedV1}
	decoders := event.NewRegistry()
	decoders.Register(decoder)
	handler := CloudEventsHandler(decoders, &successChain{}, zap.NewNop(), testCollectors(t), true, nil)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ce-Id", "evt-raw-1")
	req.Header.Set("Ce-Type", testTaskRunStartedV1)
	req.Header.Set("Ce-Source", "tekton.dev/pipelines")
	req.Header.Set("Ce-Specversion", "1.0")
	rec := httptest.NewRecorder()

	// When: the receiver parses the CloudEvent and dispatches it through decodeEvent.
	handler(rec, req)

	// Then: the request is acknowledged and RawEvent still contains the exact body bytes.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if string(decoder.raw.Data) != body {
		t.Errorf("RawEvent.Data = %q, want %q", string(decoder.raw.Data), body)
	}
	if decoder.raw.ID != "evt-raw-1" {
		t.Errorf("RawEvent.ID = %q, want evt-raw-1", decoder.raw.ID)
	}
	if decoder.raw.Source != "tekton.dev/pipelines" {
		t.Errorf("RawEvent.Source = %q, want tekton.dev/pipelines", decoder.raw.Source)
	}
}

type capturingDecoder struct {
	eventType string
	raw       event.RawEvent
}

func (d *capturingDecoder) Name() string { return "capturing-decoder" }

func (d *capturingDecoder) CanHandle(eventType string) bool { return eventType == d.eventType }

func (d *capturingDecoder) Decode(raw event.RawEvent) (*event.Envelope, error) {
	d.raw = raw
	return &event.Envelope{
		CloudEventID:   raw.ID,
		CloudEventType: raw.Type,
		Source:         raw.Source,
		Report: domain.Event{
			Provider: captureProvider,
			RunName:  "build-123",
		},
	}, nil
}

var _ event.Decoder = (*capturingDecoder)(nil)
