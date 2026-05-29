package bitbucket

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
)

// CloudClient holds shared HTTP client and auth for Bitbucket Cloud action handlers.
type CloudClient struct {
	http        *http.Client
	username    string
	appPassword string
	baseURL     string
	maxRetries  int
	baseDelay   time.Duration
}

// NewCloudClient creates a new Bitbucket Cloud API client.
func NewCloudClient(username, appPassword, baseURL string, insecureSkipVerify bool) *CloudClient {
	if baseURL == "" {
		baseURL = "https://api.bitbucket.org"
	}
	return &CloudClient{
		http:        httpx.NewClient(httpx.ClientConfig{
			Timeout:            10 * time.Second,
			InsecureSkipVerify: insecureSkipVerify,
		}),
		username:    username,
		appPassword: appPassword,
		baseURL:     baseURL,
		maxRetries:  3,
		baseDelay:   100 * time.Millisecond,
	}
}

// Do performs an HTTP request with Bitbucket Cloud authentication and JSON encoding.
func (c *CloudClient) Do(ctx context.Context, method, url string, payload any) error {
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

	cred := c.username + ":" + c.appPassword
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cred)))
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
