package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitMiddleware_WithinLimit(t *testing.T) {
	handler := NewRateLimiter(10, 5).Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Ce-Source", "test-source")

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rec.Code)
		}
	}
}

func TestRateLimitMiddleware_ExceedsLimit(t *testing.T) {
	handler := NewRateLimiter(1, 2).Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Ce-Source", "test-source")

	for i := range 2 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
}

func TestRateLimitMiddleware_DifferentRemoteAddrHosts(t *testing.T) {
	handler := NewRateLimiter(1, 1).Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest("POST", "/", nil)
	req1.RemoteAddr = "192.0.2.10:1000"
	req1.Header.Set("Ce-Source", "same-spoofed-source")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest("POST", "/", nil)
	req2.RemoteAddr = "192.0.2.11:1000"
	req2.Header.Set("Ce-Source", "same-spoofed-source")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK {
		t.Error("different remote address hosts should have separate rate limits")
	}
}

func TestRateLimitMiddleware_BaselineCeSourceControlsBucketBeforeHardening(t *testing.T) {
	t.Skip("pre-hardening characterization is preserved in .omo/evidence/task-2-sonnet-full-application-audit.md")

	// Given
	handler := NewRateLimiter(1, 1).Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	first := httptest.NewRequest("POST", "/", nil)
	first.RemoteAddr = "192.0.2.10:1000"
	first.Header.Set("Ce-Source", "spoofed-source-1")
	second := httptest.NewRequest("POST", "/", nil)
	second.RemoteAddr = "192.0.2.10:1001"
	second.Header.Set("Ce-Source", "spoofed-source-2")

	// When
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, first)
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, second)

	// Then
	if firstRec.Code != http.StatusOK || secondRec.Code != http.StatusOK {
		t.Fatalf("pre-hardening Ce-Source keying should allow both requests, got %d and %d", firstRec.Code, secondRec.Code)
	}
}

func TestRateLimitMiddleware_IdentityUsesRemoteAddrHostOnly(t *testing.T) {
	// Given
	handler := NewRateLimiter(1, 1).Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	first := httptest.NewRequest("POST", "/", nil)
	first.RemoteAddr = "192.0.2.10:1000"
	first.Header.Set("Ce-Source", "spoofed-source-1")
	first.Header.Set("X-Forwarded-For", "198.51.100.1")
	second := httptest.NewRequest("POST", "/", nil)
	second.RemoteAddr = "192.0.2.10:1001"
	second.Header.Set("Ce-Source", "spoofed-source-2")
	second.Header.Set("X-Forwarded-For", "198.51.100.2")

	// When
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, first)
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, second)

	// Then
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first request should pass, got %d", firstRec.Code)
	}
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request from same RemoteAddr host should be throttled, got %d", secondRec.Code)
	}
}

func TestRateLimitMiddleware_FallbackToIP(t *testing.T) {
	handler := NewRateLimiter(1, 1).Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "192.168.1.1:1234"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
