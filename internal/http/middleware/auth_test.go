package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testSecret     = "my-secret"
	testData       = `{"test":"data"}`
	authTypeHMAC   = "hmac-sha256"
	authTypeBearer = "bearer"
)

func TestAuthMiddleware_HMAC_Valid(t *testing.T) {
	config := AuthConfig{Type: authTypeHMAC, Secret: testSecret}

	mw, err := AuthMiddleware(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := []byte(testData)

	// Compute valid signature
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddleware_HMAC_Invalid(t *testing.T) {
	config := AuthConfig{Type: authTypeHMAC, Secret: testSecret}

	mw, err := AuthMiddleware(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", strings.NewReader(testData))
	req.Header.Set("X-Hub-Signature-256", "sha256=wrong_signature")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_HMAC_MissingHeader(t *testing.T) {
	config := AuthConfig{Type: authTypeHMAC, Secret: testSecret}

	mw, err := AuthMiddleware(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", strings.NewReader(testData))
	// No X-Hub-Signature-256 header
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_Bearer_Valid(t *testing.T) {
	token := "secret-token-123"
	config := AuthConfig{Type: authTypeBearer, Secret: token}

	mw, err := AuthMiddleware(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddleware_Bearer_Invalid(t *testing.T) {
	config := AuthConfig{Type: authTypeBearer, Secret: "correct-token"}

	mw, err := AuthMiddleware(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_Bearer_MissingHeader(t *testing.T) {
	config := AuthConfig{Type: authTypeBearer, Secret: "token"}

	mw, err := AuthMiddleware(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", nil)
	// No Authorization header
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
