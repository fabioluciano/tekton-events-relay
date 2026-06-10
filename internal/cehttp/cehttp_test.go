package cehttp

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestFromRequest_ValidCloudEvent(t *testing.T) {
	body := `{"key":"value"}`
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ce-Id", "123")
	req.Header.Set("Ce-Type", "test.event")
	req.Header.Set("Ce-Source", "test")
	req.Header.Set("Ce-Specversion", "1.0")

	ce, err := FromRequest(req)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if ce.ID != "123" {
		t.Errorf("ID = %q, want 123", ce.ID)
	}
	if ce.Type != "test.event" {
		t.Errorf("Type = %q, want test.event", ce.Type)
	}
	if ce.Source != "test" {
		t.Errorf("Source = %q, want test", ce.Source)
	}
}

func TestFromRequest_MissingID(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Ce-Type", "test.event")
	req.Header.Set("Ce-Source", "test")

	_, err := FromRequest(req)
	if err == nil {
		t.Error("expected error for missing Ce-Id")
	}
}

func TestFromRequest_MissingType(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Ce-Id", "123")
	req.Header.Set("Ce-Source", "test")

	_, err := FromRequest(req)
	if err == nil {
		t.Error("expected error for missing Ce-Type")
	}
}

func TestFromRequest_MissingSource(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Ce-Id", "123")
	req.Header.Set("Ce-Type", "test.event")

	_, err := FromRequest(req)
	if err == nil {
		t.Error("expected error for missing Ce-Source")
	}
}

func TestFromRequest_WithBody(t *testing.T) {
	body := `{"pipeline":"run-123"}`
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ce-Id", "456")
	req.Header.Set("Ce-Type", "pipeline.completed")
	req.Header.Set("Ce-Source", "tekton")
	req.Header.Set("Ce-Specversion", "1.0")

	ce, err := FromRequest(req)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if string(ce.Data) != body {
		t.Errorf("Data = %q, want %q", string(ce.Data), body)
	}
}

func TestFromRequest_StructuredMode(t *testing.T) {
	// Structured-mode CloudEvent: entire event is in the body with Content-Type: application/cloudevents+json
	body := `{
		"specversion": "1.0",
		"id": "789",
		"type": "structured.event",
		"source": "test-source",
		"subject": "test-subject",
		"time": "2026-06-09T12:00:00Z",
		"data": {"key": "value"}
	}`
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/cloudevents+json")

	ce, err := FromRequest(req)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if ce.ID != "789" {
		t.Errorf("ID = %q, want 789", ce.ID)
	}
	if ce.Type != "structured.event" {
		t.Errorf("Type = %q, want structured.event", ce.Type)
	}
	if ce.Source != "test-source" {
		t.Errorf("Source = %q, want test-source", ce.Source)
	}
	if ce.Subject != "test-subject" {
		t.Errorf("Subject = %q, want test-subject", ce.Subject)
	}
	if ce.Time != "2026-06-09T12:00:00Z" {
		t.Errorf("Time = %q, want 2026-06-09T12:00:00Z", ce.Time)
	}
}
