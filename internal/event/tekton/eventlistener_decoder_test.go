package tekton

import (
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

const (
	typeEventListenerStarted    = "dev.tekton.event.triggers.started.v1"
	typeEventListenerSuccessful = "dev.tekton.event.triggers.successful.v1"
	typeEventListenerFailed     = "dev.tekton.event.triggers.failed.v1"
	typeEventListenerDone       = "dev.tekton.event.triggers.done.v1"

	testEventIDEventlistener = "evt-1"
)

func TestEventListenerDecoder_Name(t *testing.T) {
	d := NewEventListenerDecoder()
	if d.Name() != decoderNameEventListener {
		t.Errorf("Name() = %q, want %s", d.Name(), decoderNameEventListener)
	}
}

func TestEventListenerDecoder_CanHandle(t *testing.T) {
	d := NewEventListenerDecoder()

	tests := []struct {
		eventType string
		want      bool
	}{
		{"dev.tekton.event.triggers.started.v1", true},
		{"dev.tekton.event.triggers.successful.v1", true},
		{"dev.tekton.event.triggers.failed.v1", true},
		{"dev.tekton.event.triggers.done.v1", true},
		{"dev.tekton.event.taskrun.successful.v1", false}, //nolint:goconst
		{"dev.tekton.event.pipelinerun.successful.v1", false},
		{"dev.tekton.event.customrun.successful.v1", false},
		{"io.example.foreign.v1", false},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			if got := d.CanHandle(tt.eventType); got != tt.want {
				t.Errorf("CanHandle(%q) = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestEventListenerDecoder_Decode_Started(t *testing.T) {
	// started.v1 payload is the incoming webhook HTTP headers (map[string][]string)
	payload := `{"X-Github-Event":["` + canonicalEventPullRequest + `"],"X-Hub-Signature-256":["sha256=abc123"]}`

	d := NewEventListenerDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:     "evt-el-1",
		Type:   typeEventListenerStarted,
		Source: "/apis/triggers.tekton.dev/v1alpha1/namespaces/tekton-pipelines/eventlisteners/github-listener",
		Data:   []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if env.Report.Resource != domain.ResourceEventListener {
		t.Errorf("Resource = %q, want eventlistener", env.Report.Resource)
	}
	if env.Report.State != domain.StateRunning {
		t.Errorf("State = %q, want running", env.Report.State)
	}
	// EventListenerName extracted from last non-empty segment of Source URL
	if env.Report.EventListenerName != "github-listener" {
		t.Errorf("EventListenerName = %q, want github-listener", env.Report.EventListenerName)
	}
	// RunID is the CloudEvent ID for started events
	if env.Report.RunID != "evt-el-1" {
		t.Errorf("RunID = %q, want evt-el-1", env.Report.RunID)
	}
	if env.Report.Provider != "github" {
		t.Errorf("Provider = %q, want github", env.Report.Provider)
	}
	if env.Report.SCMEventType != canonicalEventPullRequest {
		t.Errorf("SCMEventType = %q, want %s", env.Report.SCMEventType, canonicalEventPullRequest)
	}
	if env.CloudEventID != "evt-el-1" {
		t.Errorf("CloudEventID = %q", env.CloudEventID)
	}
}

func TestEventListenerDecoder_Decode_Successful(t *testing.T) {
	payload := `{
  "eventListener": "gitlab-listener",
  "namespace": "default",
  "eventListenerUID": "el-uid-789",
  "eventID": "event-abc"
}`

	d := NewEventListenerDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-el-2",
		Type: typeEventListenerSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if env.Report.State != domain.StateSuccess {
		t.Errorf("State = %q, want success", env.Report.State)
	}
	if env.Report.Description != "EventListener processing successful" {
		t.Errorf("Description = %q", env.Report.Description)
	}
}

func TestEventListenerDecoder_Decode_Failed(t *testing.T) {
	payload := `{
  "eventListener": "bitbucket-listener",
  "namespace": "ci",
  "eventListenerUID": "el-uid-fail",
  "eventID": "event-fail"
}`

	d := NewEventListenerDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-el-3",
		Type: typeEventListenerFailed,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if env.Report.State != domain.StateFailure {
		t.Errorf("State = %q, want failure", env.Report.State)
	}
	if env.Report.Description != "EventListener processing failed" {
		t.Errorf("Description = %q", env.Report.Description)
	}
}

func TestEventListenerDecoder_Decode_Done(t *testing.T) {
	// done.v1 payload is null — no useful data, decoder returns error
	d := NewEventListenerDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-el-4",
		Type: typeEventListenerDone,
		Data: []byte(`null`),
	})
	if err == nil {
		t.Fatal("expected error for done event (no data)")
	}
}

func TestEventListenerDecoder_AllStates(t *testing.T) {
	// successful and failed use lifecycle payload
	lifecyclePayload := `{
  "eventListener": "test-listener",
  "namespace": "test",
  "eventListenerUID": "el-uid-test",
  "eventID": "event-test"
}`
	lifecycleTests := []struct {
		eventType string
		want      domain.State
		desc      string
	}{
		{typeEventListenerSuccessful, domain.StateSuccess, "EventListener processing successful"},
		{typeEventListenerFailed, domain.StateFailure, "EventListener processing failed"},
	}

	for _, tt := range lifecycleTests {
		t.Run(tt.eventType, func(t *testing.T) {
			d := NewEventListenerDecoder()
			env, err := d.Decode(event.RawEvent{
				ID:   testEventIDEventlistener,
				Type: tt.eventType,
				Data: []byte(lifecyclePayload),
			})
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}
			if env.Report.State != tt.want {
				t.Errorf("State = %q, want %q", env.Report.State, tt.want)
			}
			if env.Report.Description != tt.desc {
				t.Errorf("Description = %q, want %q", env.Report.Description, tt.desc)
			}
		})
	}

	// started uses headers payload
	t.Run(typeEventListenerStarted, func(t *testing.T) {
		d := NewEventListenerDecoder()
		env, err := d.Decode(event.RawEvent{
			ID:     testEventIDEventlistener,
			Type:   typeEventListenerStarted,
			Source: "http://el-test-listener.ns.svc/",
			Data:   []byte(`{"X-Github-Event":["push"]}`),
		})
		if err != nil {
			t.Fatalf("Decode failed: %v", err)
		}
		if env.Report.State != domain.StateRunning {
			t.Errorf("State = %q, want running", env.Report.State)
		}
	})

	// done returns error (null payload)
	t.Run(typeEventListenerDone, func(t *testing.T) {
		d := NewEventListenerDecoder()
		_, err := d.Decode(event.RawEvent{
			ID:   testEventIDEventlistener,
			Type: typeEventListenerDone,
			Data: []byte(`null`),
		})
		if err == nil {
			t.Fatal("expected error for done event")
		}
	})
}

func TestEventListenerDecoder_MissingEventListener(t *testing.T) {
	payload := `{
  "namespace": "test",
  "eventListenerUID": "el-uid-test",
  "eventID": "event-test"
}`

	d := NewEventListenerDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1", //nolint:goconst
		Type: typeEventListenerSuccessful,
		Data: []byte(payload),
	})
	if err == nil {
		t.Fatal("expected error for missing eventListener field")
	}
	if err.Error() != "missing eventListener field" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEventListenerDecoder_MissingNamespace(t *testing.T) {
	payload := `{
  "eventListener": "test-listener",
  "eventListenerUID": "el-uid-test",
  "eventID": "event-test"
}`

	d := NewEventListenerDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeEventListenerSuccessful,
		Data: []byte(payload),
	})
	if err == nil {
		t.Fatal("expected error for missing namespace field")
	}
	if err.Error() != "missing namespace field" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEventListenerDecoder_MissingUID(t *testing.T) {
	payload := `{
  "eventListener": "test-listener",
  "namespace": "test",
  "eventID": "event-test"
}`

	d := NewEventListenerDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeEventListenerSuccessful,
		Data: []byte(payload),
	})
	if err == nil {
		t.Fatal("expected error for missing eventListenerUID field")
	}
	if err.Error() != "missing eventListenerUID field" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEventListenerDecoder_InvalidJSON(t *testing.T) {
	d := NewEventListenerDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeEventListenerStarted,
		Data: []byte(`{invalid json`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestEventListenerDecoder_InvalidEventType(t *testing.T) {
	payload := `{
  "eventListener": "test-listener",
  "namespace": "test",
  "eventListenerUID": "el-uid-test",
  "eventID": "event-test"
}`

	d := NewEventListenerDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: "dev.tekton.event.taskrun.successful.v1",
		Data: []byte(payload),
	})
	if err == nil {
		t.Fatal("expected error for non-eventlistener event type")
	}
}

func TestEventListenerDecoder_EmptyPayload(t *testing.T) {
	d := NewEventListenerDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeEventListenerStarted,
		Data: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
}
