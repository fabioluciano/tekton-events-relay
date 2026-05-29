package httpx

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoWithRetry_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, err := DoWithRetry(nil, req, 3, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestDoWithRetry_TransientRetry(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, err := DoWithRetry(nil, req, 5, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestDoWithRetry_MaxAttemptsExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	_, err := DoWithRetry(nil, req, 2, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error after max attempts")
	}
}

func TestDoWithRetry_WithBody(t *testing.T) {
	var attempts atomic.Int32
	var receivedBodies []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBodies = append(receivedBodies, string(body))

		if attempts.Add(1) < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	bodyContent := "test payload"
	req, _ := http.NewRequest("POST", server.URL, bytes.NewReader([]byte(bodyContent)))
	resp, err := DoWithRetry(nil, req, 3, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if len(receivedBodies) != 2 {
		t.Errorf("expected 2 bodies received, got %d", len(receivedBodies))
	}
	for i, body := range receivedBodies {
		if body != bodyContent {
			t.Errorf("attempt %d: body = %q, want %q", i+1, body, bodyContent)
		}
	}
}

func TestDoWithRetry_CustomClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	customClient := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", server.URL, nil)
	_, err := DoWithRetry(customClient, req, 1, 10*time.Millisecond)
	if err != nil {
		t.Errorf("expected success with custom client, got error: %v", err)
	}
}

func TestIsTransient(t *testing.T) {
	tests := []struct {
		code      int
		transient bool
	}{
		{200, false},
		{201, false},
		{400, false},
		{404, false},
		{408, true},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.code), func(t *testing.T) {
			result := isTransient(tt.code)
			if result != tt.transient {
				t.Errorf("isTransient(%d) = %v, want %v", tt.code, result, tt.transient)
			}
		})
	}
}

func TestDoWithRetry_NetworkError(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://localhost", nil)
	_, err := DoWithRetry(&http.Client{Transport: &errorTransport{}}, req, 2, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for network failure")
	}
}

type errorTransport struct{}

func (e *errorTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, errors.New("transport error")
}
