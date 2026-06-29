package cehttp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFromRequest_BinaryModeCharacterizesCurrentReceiveBehavior(t *testing.T) {
	// Given: a binary-mode CloudEvent request in the shape Tekton posts to the receiver.
	body := `{"pipelineRun":{"metadata":{"name":"build-123"}}}`
	req := newBinaryRequest(body)
	req.Header.Set("Ce-Id", "evt-binary-1")
	req.Header.Set("Ce-Type", "dev.tekton.event.pipelinerun.started.v1")
	req.Header.Set("Ce-Source", "tekton.dev/pipelines")
	req.Header.Set("Ce-Subject", "ci/build-123")
	req.Header.Set("Ce-Time", "2026-06-09T12:00:00Z")

	// When: the request is converted through the official CloudEvents HTTP binding.
	ce, err := FromRequest(req)

	// Then: CloudEvent attributes and the raw payload bytes are preserved for downstream decoders.
	if err != nil {
		t.Fatalf("FromRequest returned error: %v", err)
	}
	assertEvent(t, ce, eventWant{
		id:          "evt-binary-1",
		typeName:    "dev.tekton.event.pipelinerun.started.v1",
		source:      "tekton.dev/pipelines",
		specVersion: "1.0",
		subject:     "ci/build-123",
		time:        "2026-06-09T12:00:00Z",
		data:        body,
	})
}

func TestFromRequest_StructuredModeParsesJSONEvent(t *testing.T) {
	// Given: a structured-mode CloudEvent where metadata and data share the JSON envelope.
	body := `{"specversion":"1.0","id":"evt-structured-1","type":"dev.tekton.event.taskrun.started.v1","source":"tekton.dev/pipelines","subject":"ci/task-1","time":"2026-06-09T12:00:00Z","data":{"taskRun":{"metadata":{"name":"task-1"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/cloudevents+json")

	// When: the request is parsed as a CloudEvent.
	ce, err := FromRequest(req)

	// Then: structured attributes are decoded and only the data object is exposed to pipeline decoders.
	if err != nil {
		t.Fatalf("FromRequest returned error: %v", err)
	}
	assertEvent(t, ce, eventWant{
		id:          "evt-structured-1",
		typeName:    "dev.tekton.event.taskrun.started.v1",
		source:      "tekton.dev/pipelines",
		specVersion: "1.0",
		subject:     "ci/task-1",
		time:        "2026-06-09T12:00:00Z",
		data:        `{"taskRun":{"metadata":{"name":"task-1"}}}`,
	})
}

func TestFromRequest_InvalidCloudEventsFail(t *testing.T) {
	tests := []struct {
		name string
		req  *http.Request
	}{
		{
			name: "binary missing id",
			req: func() *http.Request {
				req := newBinaryRequest(`{"key":"value"}`)
				req.Header.Del("Ce-Id")
				return req
			}(),
		},
		{
			name: "binary missing type",
			req: func() *http.Request {
				req := newBinaryRequest(`{"key":"value"}`)
				req.Header.Del("Ce-Type")
				return req
			}(),
		},
		{
			name: "binary missing source",
			req: func() *http.Request {
				req := newBinaryRequest(`{"key":"value"}`)
				req.Header.Del("Ce-Source")
				return req
			}(),
		},
		{
			name: "binary missing specversion",
			req: func() *http.Request {
				req := newBinaryRequest(`{"key":"value"}`)
				req.Header.Del("Ce-Specversion")
				return req
			}(),
		},
		{
			name: "structured missing id",
			req:  newStructuredRequest(`{"specversion":"1.0","type":"test.event","source":"test","data":{}}`),
		},
		{
			name: "structured missing type",
			req:  newStructuredRequest(`{"specversion":"1.0","id":"evt-1","source":"test","data":{}}`),
		},
		{
			name: "structured missing source",
			req:  newStructuredRequest(`{"specversion":"1.0","id":"evt-1","type":"test.event","data":{}}`),
		},
		{
			name: "structured missing specversion",
			req:  newStructuredRequest(`{"id":"evt-1","type":"test.event","source":"test","data":{}}`),
		},
		{
			name: "structured malformed json",
			req:  newStructuredRequest(`{"specversion":"1.0","id":"evt-1"`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When: invalid CloudEvent input crosses the HTTP parsing boundary.
			_, err := FromRequest(tt.req)

			// Then: the SDK-backed parser rejects it instead of fabricating a partial event.
			if err == nil {
				t.Fatal("expected FromRequest to reject invalid CloudEvent")
			}
		})
	}
}

type eventWant struct {
	id          string
	typeName    string
	source      string
	specVersion string
	subject     string
	time        string
	data        string
}

func assertEvent(t *testing.T, got *Event, want eventWant) {
	t.Helper()
	if got.ID != want.id {
		t.Errorf("ID = %q, want %q", got.ID, want.id)
	}
	if got.Type != want.typeName {
		t.Errorf("Type = %q, want %q", got.Type, want.typeName)
	}
	if got.Source != want.source {
		t.Errorf("Source = %q, want %q", got.Source, want.source)
	}
	if got.SpecVersion != want.specVersion {
		t.Errorf("SpecVersion = %q, want %q", got.SpecVersion, want.specVersion)
	}
	if got.Subject != want.subject {
		t.Errorf("Subject = %q, want %q", got.Subject, want.subject)
	}
	if got.Time != want.time {
		t.Errorf("Time = %q, want %q", got.Time, want.time)
	}
	if string(got.Data) != want.data {
		t.Errorf("Data = %q, want %q", string(got.Data), want.data)
	}
}

func newBinaryRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ce-Id", "evt-1")
	req.Header.Set("Ce-Type", "test.event")
	req.Header.Set("Ce-Source", "test-source")
	req.Header.Set("Ce-Specversion", "1.0")
	return req
}

func newStructuredRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/cloudevents+json")
	return req
}
