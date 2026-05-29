// Package httpx contains HTTP utilities reused by all adapters.
package httpx

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DoWithRetry executes the request with exponential backoff on transient errors.
// Considers transient: network error, 408, 429, 5xx.
func DoWithRetry(c *http.Client, req *http.Request, maxAttempts int, baseDelay time.Duration) (*http.Response, error) {
	if c == nil {
		c = http.DefaultClient
	}

	// Capture body to be able to resend on retry.
	var bodyBytes []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("read body for retry: %w", err)
		}
		bodyBytes = b
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	var lastErr error
	delay := baseDelay
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 && bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
		//nolint:gosec // G107: SSRF risk is acceptable here as URLs are controlled by configuration
		resp, err := c.Do(req)
		switch {
		case err != nil:
			lastErr = err
		case isTransient(resp.StatusCode):
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("transient status %d", resp.StatusCode)
		default:
			return resp, nil
		}

		if attempt < maxAttempts {
			time.Sleep(delay)
			delay *= 2
		}
	}
	return nil, fmt.Errorf("after %d attempts: %w", maxAttempts, lastErr)
}

func isTransient(code int) bool {
	return code == http.StatusRequestTimeout ||
		code == http.StatusTooManyRequests ||
		(code >= 500 && code < 600)
}
