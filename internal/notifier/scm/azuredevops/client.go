package azuredevops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
)

// Client holds shared HTTP client and auth for all Azure DevOps action handlers.
type Client struct {
	http       *http.Client
	token      string
	baseURL    string
	genre      string
	maxRetries int
	baseDelay  time.Duration
}

// NewClient creates a new Azure DevOps API client.
func NewClient(token, baseURL, genre string, insecureSkipVerify bool) *Client {
	if baseURL == "" {
		baseURL = "https://dev.azure.com"
	}
	if genre == "" {
		genre = "tekton-ci"
	}
	return &Client{
		http: httpx.NewClient(httpx.ClientConfig{
			Timeout:            10 * time.Second,
			InsecureSkipVerify: insecureSkipVerify,
		}),
		token:      token,
		baseURL:    baseURL,
		genre:      genre,
		maxRetries: 3,
		baseDelay:  100 * time.Millisecond,
	}
}

// Do performs an HTTP request with Azure DevOps authentication and JSON encoding.
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

	req.SetBasicAuth("", c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tekton-events-relay")

	resp, err := httpx.DoWithRetry(c.http, req, c.maxRetries, c.baseDelay)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("azure devops API returned %d", resp.StatusCode)
	}

	return nil
}
