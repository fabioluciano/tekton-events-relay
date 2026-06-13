// Package gitlab provides GitLab SCM notifier client.
package gitlab

import (
	"fmt"
	"time"

	gl "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
)

// Client wraps the official GitLab SDK client.
type Client struct {
	gl  *gl.Client
	log *zap.Logger
}

// NewClient creates a new GitLab API client with the given token and base URL.
func NewClient(token, baseURL string, insecureSkipVerify bool, debug bool, log *zap.Logger) (*Client, error) {
	if log == nil {
		log = zap.NewNop()
	}

	// Build HTTP client with custom options
	httpOpts := []httpx.Option{httpx.WithTimeout(10 * time.Second)}
	if insecureSkipVerify {
		httpOpts = append(httpOpts, httpx.WithInsecureSkipVerify())
	}
	if debug {
		httpOpts = append(httpOpts, httpx.WithDebug(log, "gitlab"))
	}
	httpClient := httpx.NewClient(httpOpts...)

	// Configure GitLab SDK client
	opts := []gl.ClientOptionFunc{
		gl.WithHTTPClient(httpClient),
	}
	if baseURL != "" {
		opts = append(opts, gl.WithBaseURL(baseURL))
	}

	glClient, err := gl.NewClient(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("create GitLab client: %w", err)
	}

	return &Client{
		gl:  glClient,
		log: log,
	}, nil
}
