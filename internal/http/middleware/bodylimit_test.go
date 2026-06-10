package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBodyLimitMiddleware_WithinLimit(t *testing.T) {
	handler := BodyLimitMiddleware(1024)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", strings.NewReader("small body"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestBodyLimitMiddleware_ExceedsLimit(t *testing.T) {
	handler := BodyLimitMiddleware(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to read body
		buf := make([]byte, 100)
		_, err := r.Body.Read(buf)
		if err == nil {
			t.Error("expected error reading oversized body")
		}
		w.WriteHeader(http.StatusOK)
	}))

	bigBody := strings.Repeat("x", 100) // 100 bytes, limit is 10
	req := httptest.NewRequest("POST", "/", strings.NewReader(bigBody))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// MaxBytesReader causes error when body read exceeds limit
	// Handler should handle gracefully
}

func TestBodyLimitMiddleware_Default1MB(t *testing.T) {
	const oneMB = 1048576

	handler := BodyLimitMiddleware(oneMB)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Body just under 1MB
	body := bytes.Repeat([]byte("x"), oneMB-1)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
