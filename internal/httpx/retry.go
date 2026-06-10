// Package httpx contains HTTP utilities reused by all adapters.
package httpx

import (
	"context"
	"fmt"
	"net/http"
	"time"

	retryhttp "github.com/hashicorp/go-retryablehttp"

	"github.com/fabioluciano/tekton-events-relay/internal/errors"
)

// DoWithRetry executes req with exponential backoff via go-retryablehttp.
// Retries on 408, 429, 5xx, and network errors. Respects Retry-After header.
func DoWithRetry(c *http.Client, req *http.Request, maxAttempts int, baseDelay time.Duration) (*http.Response, error) {
	if c == nil {
		c = http.DefaultClient
	}

	rc := retryhttp.NewClient()
	rc.RetryMax = maxAttempts - 1
	rc.RetryWaitMin = baseDelay
	rc.RetryWaitMax = baseDelay * time.Duration(1<<uint(maxAttempts))
	rc.HTTPClient = c
	rc.Logger = nil // suppress retryablehttp's default stderr logging

	rc.CheckRetry = func(_ context.Context, resp *http.Response, err error) (bool, error) {
		if err != nil {
			return true, nil //nolint:nilerr
		}
		if resp != nil && (resp.StatusCode == 408 ||
			resp.StatusCode == 429 ||
			(resp.StatusCode >= 500 && resp.StatusCode < 600)) {
			return true, nil
		}
		return false, nil
	}

	retryReq, err := retryhttp.FromRequest(req)
	if err != nil {
		return nil, err
	}

	resp, err := rc.Do(retryReq)
	if err != nil {
		return nil, errors.NewRetryable(
			fmt.Errorf("after %d attempts: %w", maxAttempts, err),
			"max_retries",
		)
	}
	return resp, nil
}
