package httpx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	internalerrors "github.com/fabioluciano/tekton-events-relay/internal/errors"
)

func TestDoWithRetry_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
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

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
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

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := DoWithRetry(nil, req, 2, 10*time.Millisecond)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
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
	req, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL, bytes.NewReader([]byte(bodyContent)))
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
	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := DoWithRetry(customClient, req, 1, 10*time.Millisecond)
	if err != nil {
		t.Errorf("expected success with custom client, got error: %v", err)
	}
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
}

func TestDoWithRetry_NetworkError(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://localhost", nil)
	resp, err := DoWithRetry(&http.Client{Transport: &errorTransport{}}, req, 2, 10*time.Millisecond)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err == nil {
		t.Fatal("expected error for network failure")
	}
}

type errorTransport struct{}

func (e *errorTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, errors.New("transport error")
}

func TestDoWithRetry_ReturnsRetryableAfterMaxAttempts(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := DoWithRetry(nil, req, 3, 10*time.Millisecond)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	if err == nil {
		t.Fatal("expected error after max retries")
	}

	if !internalerrors.IsRetryable(err) {
		t.Errorf("error should be retryable, got: %T", err)
	}

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestDoWithRetry_ContextCancellation(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)

	// Cancel context after first attempt completes
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	resp, err := DoWithRetry(nil, req, 5, 100*time.Millisecond)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	if err == nil {
		t.Error("expected error after context cancellation")
	}

	// Should exit early, not attempt all 5 retries
	if attempts.Load() >= 5 {
		t.Errorf("expected early exit, but got %d attempts", attempts.Load())
	}
}

func TestDoWithRetry_ContextCancellationDuringSleep(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)

	// Cancel during the sleep between retries
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	resp, err := DoWithRetry(nil, req, 5, 200*time.Millisecond)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	if err == nil {
		t.Error("expected error after context cancellation")
	}

	// Should exit during sleep after first or second attempt
	if attempts.Load() >= 3 {
		t.Errorf("expected early exit during sleep, but got %d attempts", attempts.Load())
	}
}
