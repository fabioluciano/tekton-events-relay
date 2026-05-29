package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
)

// ServerClient holds shared HTTP client and auth for Bitbucket Server action handlers.
type ServerClient struct {
	http       *http.Client
	token      string
	baseURL    string
	maxRetries int
	baseDelay  time.Duration
}

// NewServerClient creates a new Bitbucket Server API client.
func NewServerClient(token, baseURL string, insecureSkipVerify bool) *ServerClient {
	return &ServerClient{
		http:       httpx.NewClient(httpx.ClientConfig{
			Timeout:            10 * time.Second,
			InsecureSkipVerify: insecureSkipVerify,
		}),
		token:      token,
		baseURL:    baseURL,
		maxRetries: 3,
		baseDelay:  100 * time.Millisecond,
	}
}

// Do performs an HTTP request with Bitbucket Server authentication and JSON encoding.
func (c *ServerClient) Do(ctx context.Context, method, url string, payload any) error {
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			return fmt.Errorf("encode payload: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, url, &body)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tekton-events-relay")

	resp, err := httpx.DoWithRetry(c.http, req, c.maxRetries, c.baseDelay)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("bitbucket API returned %d", resp.StatusCode)
	}

	return nil
}
