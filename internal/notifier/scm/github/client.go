package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
)

// Client holds shared HTTP client and auth for all GitHub action handlers.
// Prevents connection waste when multiple handlers are registered.
type Client struct {
	http       *http.Client
	token      string
	baseURL    string
	maxRetries int
	baseDelay  time.Duration
}

// NewClient creates a new GitHub API client with the given token, base URL, and TLS verification setting.
func NewClient(token, baseURL string, insecureSkipVerify bool) *Client {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &Client{
		http: httpx.NewClient(httpx.ClientConfig{
			Timeout:            10 * time.Second,
			InsecureSkipVerify: insecureSkipVerify,
		}),
		token:      token,
		baseURL:    baseURL,
		maxRetries: 3,
		baseDelay:  100 * time.Millisecond,
	}
}

// Do performs an HTTP request with GitHub authentication and JSON encoding.
func (c *Client) Do(ctx context.Context, method, url string, payload any) error {
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

	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tekton-events-relay")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := httpx.DoWithRetry(c.http, req, c.maxRetries, c.baseDelay)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("github API returned %d", resp.StatusCode)
	}

	return nil
}
