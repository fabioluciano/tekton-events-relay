package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func TestNew(t *testing.T) {
	cfg := Config{URL: "https://example.com/webhook"}
	n := New(cfg)

	if n == nil {
		t.Fatal("expected notifier, got nil")
	}
	if n.cfg.URL != cfg.URL {
		t.Errorf("URL = %q, want %q", n.cfg.URL, cfg.URL)
	}
}

func TestName(t *testing.T) {
	n := New(Config{URL: "https://test"})
	if n.Name() != "webhook" {
		t.Errorf("Name() = %q, want webhook", n.Name())
	}
}

func TestNotify_Success(t *testing.T) {
	var receivedPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{URL: server.URL}
	n := New(cfg)

	evt := domain.Event{
		RunID:   "run-456",
		RunName: "run-456",
		State:   domain.StateSuccess,
		Context: "test/webhook",
	}

	err := n.Notify(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if receivedPayload == nil {
		t.Fatal("expected payload to be sent")
	}
}

func TestNotify_WithHeaders(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		URL:     server.URL,
		Headers: map[string]string{"Authorization": "Bearer test-token"},
	}
	n := New(cfg)

	evt := domain.Event{
		RunID:   "run-789",
		RunName: "run-789",
		State:   domain.StateFailure,
	}

	err := n.Notify(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if authHeader != "Bearer test-token" {
		t.Errorf("Authorization = %q, want 'Bearer test-token'", authHeader)
	}
}

func TestPayload(t *testing.T) {
	n := New(Config{URL: "https://test"})

	evt := domain.Event{
		RunID:   "run-abc",
		RunName: "run-abc",
		State:   domain.StateSuccess,
		Context: "build",
	}

	payload, err := n.payload(evt)
	if err != nil {
		t.Fatalf("payload() error: %v", err)
	}

	data, ok := payload.(map[string]interface{})
	if !ok {
		t.Fatal("payload should be a map")
	}

	if data["run_id"] != evt.RunName {
		t.Errorf("run_id = %q, want %q", data["run_id"], evt.RunName)
	}
}
