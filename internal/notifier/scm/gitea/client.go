package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
)

// Client holds shared HTTP client and auth for all Gitea action handlers.
type Client struct {
	http       *http.Client
	token      string
	baseURL    string
	maxRetries int
	baseDelay  time.Duration
}

// NewClient creates a new Gitea API client with the given token and base URL.
func NewClient(token, baseURL string, insecureSkipVerify bool) *Client {
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

// Do performs an HTTP request with Gitea authentication and JSON encoding.
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

	resp, err := httpx.DoWithRetry(c.http, req, c.maxRetries, c.baseDelay)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("gitea API returned %d", resp.StatusCode)
	}

	return nil
}
