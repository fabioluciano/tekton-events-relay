// Package httpx contains HTTP utilities reused by all adapters.
package httpx

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	retryhttp "github.com/hashicorp/go-retryablehttp"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/fabioluciano/tekton-events-relay/internal/errors"
)

// RetryPolicy configures outbound HTTP retry behavior.
type RetryPolicy struct {
	MaxAttempts    int           // total attempts including the first (default 4)
	InitialBackoff time.Duration // first backoff delay (default 250ms)
	MaxBackoff     time.Duration // backoff ceiling (default 30s)
}

// Built-in retry defaults, used when the config omits the retry block.
const (
	DefaultRetryMaxAttempts    = 4
	DefaultRetryInitialBackoff = 250 * time.Millisecond
	DefaultRetryMaxBackoff     = 30 * time.Second
)

var (
	defaultRetryClient = sync.OnceValue(func() *retryhttp.Client {
		c := retryhttp.NewClient()
		c.Logger = nil
		return c
	})

	policyMu      sync.RWMutex
	defaultPolicy = RetryPolicy{
		MaxAttempts:    DefaultRetryMaxAttempts,
		InitialBackoff: DefaultRetryInitialBackoff,
		MaxBackoff:     DefaultRetryMaxBackoff,
	}

	metricsMu     sync.RWMutex
	retriesTotal  *prometheus.CounterVec // {host, reason}
	rateLimitHits *prometheus.CounterVec // {host}
)

// SetDefaultRetryPolicy installs the process-wide retry policy from config.
// Zero or negative fields fall back to the built-in defaults.
func SetDefaultRetryPolicy(p RetryPolicy) {
	policyMu.Lock()
	defer policyMu.Unlock()
	defaultPolicy = p.normalized()
}

// DefaultRetryPolicy returns the process-wide retry policy.
func DefaultRetryPolicy() RetryPolicy {
	policyMu.RLock()
	defer policyMu.RUnlock()
	return defaultPolicy
}

// SetRetryMetrics installs the collectors used to observe retries and
// rate-limit responses. Both may be nil to disable instrumentation.
func SetRetryMetrics(retries, rateLimit *prometheus.CounterVec) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	retriesTotal = retries
	rateLimitHits = rateLimit
}

func (p RetryPolicy) normalized() RetryPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = DefaultRetryMaxAttempts
	}
	if p.InitialBackoff <= 0 {
		p.InitialBackoff = DefaultRetryInitialBackoff
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = DefaultRetryMaxBackoff
	}
	if p.MaxBackoff < p.InitialBackoff {
		p.MaxBackoff = p.InitialBackoff
	}
	return p
}

// DoWithRetryPolicy executes req honoring the given RetryPolicy.
// Retries on 408, 429, 5xx, and network errors with exponential backoff
// plus jitter. 429/503 responses with a Retry-After header are honored
// (capped at MaxBackoff). Other 4xx responses are never retried.
func DoWithRetryPolicy(c *http.Client, req *http.Request, p RetryPolicy) (*http.Response, error) {
	if c == nil {
		c = http.DefaultClient
	}
	p = p.normalized()
	host := req.URL.Host

	rc := defaultRetryClient()
	rc.RetryMax = p.MaxAttempts - 1
	rc.RetryWaitMin = p.InitialBackoff
	rc.RetryWaitMax = p.MaxBackoff
	rc.HTTPClient = c

	rc.CheckRetry = func(_ context.Context, resp *http.Response, err error) (bool, error) {
		if err != nil {
			return true, nil //nolint:nilerr
		}
		if resp != nil && (resp.StatusCode == http.StatusRequestTimeout ||
			resp.StatusCode == http.StatusTooManyRequests ||
			(resp.StatusCode >= 500 && resp.StatusCode < 600)) {
			if resp.StatusCode == http.StatusTooManyRequests {
				observeRateLimitHit(host)
			}
			return true, nil
		}
		return false, nil
	}

	// Backoff is invoked exactly once per actual retry, so it doubles as
	// the retry observation point.
	rc.Backoff = func(minWait, maxWait time.Duration, attemptNum int, resp *http.Response) time.Duration {
		observeRetry(host, retryReason(resp))
		if d, ok := retryAfterDelay(resp, maxWait); ok {
			return d
		}
		return backoffWithJitter(minWait, maxWait, attemptNum)
	}

	retryReq, err := retryhttp.FromRequest(req)
	if err != nil {
		return nil, err
	}

	resp, err := rc.Do(retryReq)
	if err != nil {
		return nil, errors.NewRetryable(
			fmt.Errorf("after %d attempts: %w", p.MaxAttempts, err),
			"max_retries",
		)
	}
	return resp, nil
}

// retryAfterDelay extracts a Retry-After delay from 429/503 responses,
// capped at maxWait. Returns false when absent or unparsable.
func retryAfterDelay(resp *http.Response, maxWait time.Duration) (time.Duration, bool) {
	if resp == nil || (resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable) {
		return 0, false
	}
	header := resp.Header.Get("Retry-After")
	if header == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(header); err == nil && secs >= 0 {
		d := time.Duration(secs) * time.Second
		if d > maxWait {
			d = maxWait
		}
		return d, true
	}
	if t, err := http.ParseTime(header); err == nil {
		d := time.Until(t)
		if d < 0 {
			d = 0
		}
		if d > maxWait {
			d = maxWait
		}
		return d, true
	}
	return 0, false
}

// backoffWithJitter computes min * 2^attempt capped at max, with the upper
// half randomized to avoid thundering herds: delay/2 + rand(delay/2).
func backoffWithJitter(minWait, maxWait time.Duration, attemptNum int) time.Duration {
	delay := minWait << uint(attemptNum)
	if delay <= 0 || delay > maxWait { // <= 0 guards shift overflow
		delay = maxWait
	}
	half := delay / 2
	return half + rand.N(half+1) // #nosec G404 -- jitter, not crypto
}

func retryReason(resp *http.Response) string {
	switch {
	case resp == nil:
		return "network_error"
	case resp.StatusCode == http.StatusTooManyRequests:
		return "rate_limit"
	case resp.StatusCode == http.StatusRequestTimeout:
		return "timeout"
	case resp.StatusCode >= 500:
		return "server_error"
	default:
		return "other"
	}
}

func observeRetry(host, reason string) {
	metricsMu.RLock()
	defer metricsMu.RUnlock()
	if retriesTotal != nil {
		retriesTotal.WithLabelValues(host, reason).Inc()
	}
}

func observeRateLimitHit(host string) {
	metricsMu.RLock()
	defer metricsMu.RUnlock()
	if rateLimitHits != nil {
		rateLimitHits.WithLabelValues(host).Inc()
	}
}
