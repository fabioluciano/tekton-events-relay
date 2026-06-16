package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const testRunName = "test"

func TestNew(t *testing.T) {
	cfg := Config{URL: "https://example.com/webhook"}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if n == nil {
		t.Fatal("expected notifier, got nil")
	}
	if n.cfg.URL != cfg.URL {
		t.Errorf("URL = %q, want %q", n.cfg.URL, cfg.URL)
	}
}

func TestName(t *testing.T) {
	n, err := New(Config{URL: "https://test"}, nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if n.Name() != "webhook" {
		t.Errorf("Name() = %q, want webhook", n.Name())
	}
}

func TestNotify_Success(t *testing.T) {
	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{URL: server.URL}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	evt := domain.Event{
		RunID:   "run-456",
		RunName: "run-456",
		State:   domain.StateSuccess,
		Context: "test/webhook",
	}

	err = n.Handle(context.Background(), evt)
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
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	evt := domain.Event{
		RunID:   "run-789",
		RunName: "run-789",
		State:   domain.StateFailure,
	}

	err = n.Handle(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if authHeader != "Bearer test-token" {
		t.Errorf("Authorization = %q, want 'Bearer test-token'", authHeader)
	}
}

func TestPayload(t *testing.T) {
	n, err := New(Config{URL: "https://test"}, nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

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

	data, ok := payload.(map[string]any)
	if !ok {
		t.Fatal("payload should be a map")
	}

	if data["run_id"] != evt.RunName {
		t.Errorf("run_id = %q, want %q", data["run_id"], evt.RunName)
	}
}

func TestNew_InvalidTransform(t *testing.T) {
	cfg := Config{
		URL:       "https://example.com/webhook",
		Transform: ". | {invalid",
	}
	_, err := New(cfg, nil)
	if err == nil {
		t.Fatal("expected error for invalid transform, got nil")
	}
	if !strings.Contains(err.Error(), "parse transform") {
		t.Errorf("error should mention parse transform, got: %v", err)
	}
}

func TestNotify_WithTransform(t *testing.T) {
	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// User's example transform from the plan
	transform := `. | {
		id:           .run_id,
		pipelineId:   .run_id,
		startedDate:  .started_at,
		finishedDate: .finished_at,
		result:       "SUCCESS",
		environment:  "PRODUCTION"
	}`

	cfg := Config{
		URL:       server.URL,
		Transform: transform,
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() with transform failed: %v", err)
	}

	evt := domain.Event{
		RunName: "test-run-123",
		State:   domain.StateSuccess,
	}

	err = n.Handle(context.Background(), evt)
	if err != nil {
		t.Fatalf("Handle() with transform failed: %v", err)
	}

	if receivedPayload["id"] != "test-run-123" {
		t.Errorf("id = %v, want 'test-run-123'", receivedPayload["id"])
	}
	if receivedPayload["result"] != "SUCCESS" {
		t.Errorf("result = %v, want 'SUCCESS'", receivedPayload["result"])
	}
	if receivedPayload["environment"] != "PRODUCTION" {
		t.Errorf("environment = %v, want 'PRODUCTION'", receivedPayload["environment"])
	}
}

func TestNotify_TransformMultipleResults_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Transform that produces multiple results
	cfg := Config{
		URL:       server.URL,
		Transform: `.run_id, .namespace`,
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{
		RunName:   testRunName,
		Namespace: "default",
	}

	err = n.Handle(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for multiple results, got nil")
	}
	if !strings.Contains(err.Error(), "produced 2 results") {
		t.Errorf("error should mention multiple results, got: %v", err)
	}
}

func TestNotify_TransformNilOutput_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Transform that produces nil
	cfg := Config{
		URL:       server.URL,
		Transform: `null`,
	}
	n, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	evt := domain.Event{RunName: testRunName}

	err = n.Handle(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for nil output, got nil")
	}
	if !strings.Contains(err.Error(), "nil output") {
		t.Errorf("error should mention nil output, got: %v", err)
	}
}

func TestNotify_BearerAuth(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		URL: server.URL,
		Auth: &ResolvedAuth{
			Type:  authTypeBearer,
			Token: scm.NewStaticToken("test-token-123"),
		},
	}
	n, _ := New(cfg, nil)

	evt := domain.Event{RunName: testRunName}
	err := n.Handle(context.Background(), evt)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}

	if authHeader != "Bearer test-token-123" {
		t.Errorf("Authorization = %q, want 'Bearer test-token-123'", authHeader)
	}
}

func TestNotify_BasicAuth(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		URL: server.URL,
		Auth: &ResolvedAuth{
			Type:     authTypeBasic,
			Username: "user",
			Password: scm.NewStaticToken("pass"),
		},
	}
	n, _ := New(cfg, nil)

	evt := domain.Event{RunName: testRunName}
	err := n.Handle(context.Background(), evt)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}

	if !strings.HasPrefix(authHeader, "Basic ") {
		t.Errorf("Authorization should start with 'Basic ', got %q", authHeader)
	}
}

func TestNotify_APIKeyAuth(t *testing.T) {
	var apiKeyHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKeyHeader = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		URL: server.URL,
		Auth: &ResolvedAuth{
			Type:   authTypeAPIKey,
			Token:  scm.NewStaticToken("secret-key"),
			Header: "X-API-Key",
		},
	}
	n, _ := New(cfg, nil)

	evt := domain.Event{RunName: testRunName}
	err := n.Handle(context.Background(), evt)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}

	if apiKeyHeader != "secret-key" {
		t.Errorf("X-API-Key = %q, want 'secret-key'", apiKeyHeader)
	}
}

func TestNotify_HMACAuth(t *testing.T) {
	var signatureHeader string
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signatureHeader = r.Header.Get("X-Webhook-Signature")
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	secret := "my-secret-key"
	cfg := Config{
		URL: server.URL,
		Auth: &ResolvedAuth{
			Type:   authTypeHMAC,
			Secret: scm.NewStaticToken(secret),
		},
	}
	n, _ := New(cfg, nil)

	evt := domain.Event{RunName: "test", State: domain.StateSuccess}
	err := n.Handle(context.Background(), evt)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}

	// Verify signature format
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		t.Errorf("signature should start with 'sha256=', got %q", signatureHeader)
	}

	// Verify signature is correct
	expectedMAC := hmac.New(sha256.New, []byte(secret))
	expectedMAC.Write(receivedBody)
	expectedSig := "sha256=" + hex.EncodeToString(expectedMAC.Sum(nil))

	if signatureHeader != expectedSig {
		t.Errorf("signature mismatch: got %q, want %q", signatureHeader, expectedSig)
	}
}

func TestNotify_HeaderConflictResolution(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Both auth and explicit Headers set Authorization - explicit Headers should win
	cfg := Config{
		URL: server.URL,
		Auth: &ResolvedAuth{
			Type:  authTypeBearer,
			Token: scm.NewStaticToken("auth-token"),
		},
		Headers: map[string]string{
			"Authorization": "Custom override-token",
		},
	}
	n, _ := New(cfg, nil)

	evt := domain.Event{RunName: testRunName}
	err := n.Handle(context.Background(), evt)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}

	// Explicit Headers should override auth-generated header
	if authHeader != "Custom override-token" {
		t.Errorf("Authorization = %q, want 'Custom override-token'", authHeader)
	}
}

func TestNotify_BackwardCompatibility_NoTransformNoAuth(t *testing.T) {
	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Old-style config with no transform or auth
	cfg := Config{URL: server.URL}
	n, _ := New(cfg, nil)

	evt := domain.Event{
		RunName:   "test-run",
		Namespace: "default",
		State:     domain.StateSuccess,
	}

	err := n.Handle(context.Background(), evt)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}

	// Verify default payload structure is unchanged
	if receivedPayload["run_id"] != "test-run" {
		t.Errorf("run_id = %v, want 'test-run'", receivedPayload["run_id"])
	}
	if receivedPayload["namespace"] != "default" {
		t.Errorf("namespace = %v, want 'default'", receivedPayload["namespace"])
	}
}
