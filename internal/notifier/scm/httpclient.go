package scm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
)

const defaultResponseBodyLimit = 4 * 1024 * 1024 // 4MB

// AuthFunc adds authentication to an HTTP request.
type AuthFunc func(req *http.Request)

// DoJSON performs an HTTP request with JSON encoding, retry logic, and optional response decoding.
func DoJSON(ctx context.Context, client *http.Client, maxRetries int, baseDelay time.Duration,
	method, url string, body any, authFn AuthFunc, v any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return fmt.Errorf("encode payload: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, url, &buf)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tekton-events-relay")

	if authFn != nil {
		authFn(req)
	}

	resp, err := httpx.DoWithRetry(client, req, maxRetries, baseDelay)
	if err != nil {
		return fmt.Errorf("http request to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, defaultResponseBodyLimit))

	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if v != nil {
		if err := json.Unmarshal(respBody, v); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}
