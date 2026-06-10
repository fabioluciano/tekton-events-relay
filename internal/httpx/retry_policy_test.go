package httpx

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestDoWithRetryPolicy_HonorsRetryAfterOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	start := time.Now()
	resp, err := DoWithRetryPolicy(nil, newRequest(t, srv.URL), RetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     5 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("elapsed = %v, want >= ~1s (Retry-After honored)", elapsed)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server calls = %d, want 2", got)
	}
}

func TestDoWithRetryPolicy_NoRetryOnClient4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	resp, err := DoWithRetryPolicy(nil, newRequest(t, srv.URL), RetryPolicy{
		MaxAttempts:    4,
		InitialBackoff: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls = %d, want 1 (4xx must not retry)", got)
	}
}

func TestDoWithRetryPolicy_ObservesMetrics(t *testing.T) {
	retries := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_retries"}, []string{"host", "reason"})
	rateLimits := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_rate_limits"}, []string{"host"})
	SetRetryMetrics(retries, rateLimits)
	defer SetRetryMetrics(nil, nil)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusInternalServerError)
		case 2:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	req := newRequest(t, srv.URL)
	resp, err := DoWithRetryPolicy(nil, req, RetryPolicy{
		MaxAttempts:    5,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	host := req.URL.Host
	if got := testutil.ToFloat64(retries.WithLabelValues(host, "server_error")); got != 1 {
		t.Errorf("retries{server_error} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(retries.WithLabelValues(host, "rate_limit")); got != 1 {
		t.Errorf("retries{rate_limit} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(rateLimits.WithLabelValues(host)); got != 1 {
		t.Errorf("rate_limit_hits = %v, want 1", got)
	}
}

func TestBackoffWithJitter_Bounds(t *testing.T) {
	const (
		minWait = 100 * time.Millisecond
		maxWait = 2 * time.Second
	)
	for attempt := range 10 {
		d := backoffWithJitter(minWait, maxWait, attempt)
		expected := minWait << uint(attempt)
		if expected <= 0 || expected > maxWait {
			expected = maxWait
		}
		if d < expected/2 || d > expected {
			t.Errorf("attempt %d: delay %v outside [%v, %v]", attempt, d, expected/2, expected)
		}
	}
}

func TestRetryPolicy_NormalizedDefaults(t *testing.T) {
	p := RetryPolicy{}.normalized()
	if p.MaxAttempts != DefaultRetryMaxAttempts {
		t.Errorf("MaxAttempts = %d, want %d", p.MaxAttempts, DefaultRetryMaxAttempts)
	}
	if p.InitialBackoff != DefaultRetryInitialBackoff {
		t.Errorf("InitialBackoff = %v, want %v", p.InitialBackoff, DefaultRetryInitialBackoff)
	}
	if p.MaxBackoff != DefaultRetryMaxBackoff {
		t.Errorf("MaxBackoff = %v, want %v", p.MaxBackoff, DefaultRetryMaxBackoff)
	}
}

func TestRetryAfterDelay_HTTPDateAndCap(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"120"}},
	}
	d, ok := retryAfterDelay(resp, 10*time.Second)
	if !ok || d != 10*time.Second {
		t.Errorf("delay = %v ok=%v, want 10s (capped) true", d, ok)
	}

	resp.Header.Set("Retry-After", "not-a-date")
	if _, ok := retryAfterDelay(resp, 10*time.Second); ok {
		t.Error("unparsable Retry-After should be ignored")
	}

	resp.StatusCode = http.StatusBadGateway
	resp.Header.Set("Retry-After", "5")
	if _, ok := retryAfterDelay(resp, 10*time.Second); ok {
		t.Error("Retry-After should only apply to 429/503")
	}
}
