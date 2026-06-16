package webhook

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

func TestApplyBearerAuth(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://example.com", nil)
	auth := &ResolvedAuth{
		Type:  authTypeBearer,
		Token: scm.NewStaticToken("test-token-123"),
	}

	err := applyAuth(req, auth)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	got := req.Header.Get("Authorization")
	want := "Bearer test-token-123"
	if got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

func TestApplyBasicAuth(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://example.com", nil)
	auth := &ResolvedAuth{
		Type:     authTypeBasic,
		Username: "admin",
		Password: scm.NewStaticToken("secret"),
	}

	err := applyAuth(req, auth)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	got := req.Header.Get("Authorization")
	// base64("admin:secret") = "YWRtaW46c2VjcmV0"
	want := "Basic YWRtaW46c2VjcmV0"
	if got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

func TestApplyAPIKeyAuth(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://example.com", nil)
	auth := &ResolvedAuth{
		Type:   authTypeAPIKey,
		Token:  scm.NewStaticToken("my-api-key"),
		Header: "X-API-Key",
	}

	err := applyAuth(req, auth)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	got := req.Header.Get("X-API-Key")
	want := "my-api-key"
	if got != want {
		t.Errorf("X-API-Key header = %q, want %q", got, want)
	}
}

func TestApplyHMACAuth(t *testing.T) {
	body := []byte(`{"test":"data"}`)
	// Use bytes.NewReader which implements io.Seeker (matches base.go:61)
	reader := bytes.NewReader(body)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://example.com", reader)
	auth := &ResolvedAuth{
		Type:   authTypeHMAC,
		Secret: scm.NewStaticToken("my-secret-key"),
	}

	err := applyAuth(req, auth)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Check signature header is set
	sig := req.Header.Get("X-Webhook-Signature")
	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("signature header should start with 'sha256=', got: %q", sig)
	}
	if len(sig) != 71 { // "sha256=" (7 chars) + 64 hex chars
		t.Errorf("signature should be 71 characters (sha256= + 64 hex), got %d: %q", len(sig), sig)
	}

	// Verify body can still be read (was reset properly)
	readBody, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read body after HMAC: %v", err)
	}
	if !bytes.Equal(readBody, body) {
		t.Errorf("body after HMAC = %q, want %q", readBody, body)
	}
}

func TestApplyAuthNil(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://example.com", nil)
	err := applyAuth(req, nil)
	if err != nil {
		t.Fatalf("expected no error for nil auth, got: %v", err)
	}
}

func TestApplyAuthUnsupportedType(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://example.com", nil)
	auth := &ResolvedAuth{
		Type: "unknown",
	}

	err := applyAuth(req, auth)
	if err == nil {
		t.Fatal("expected error for unsupported auth type, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported auth type") {
		t.Errorf("error should mention unsupported auth type, got: %v", err)
	}
}
