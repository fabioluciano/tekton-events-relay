// Package gitlab provides GitLab SCM notifier client.
package gitlab

import (
	"context"
	"fmt"
	"time"

	gl "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
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

// NewClientWithRefresher creates a GitLab API client with automatic token refresh.
// The TokenRefresher provides a fresh token for every API request, transparently
// handling OAuth2 token expiry without recreating the SDK client.
func NewClientWithRefresher(refresher scm.TokenRefresher, baseURL string, insecureSkipVerify bool, debug bool, log *zap.Logger) (*Client, error) {
	if log == nil {
		log = zap.NewNop()
	}

	httpOpts := []httpx.Option{httpx.WithTimeout(10 * time.Second)}
	if insecureSkipVerify {
		httpOpts = append(httpOpts, httpx.WithInsecureSkipVerify())
	}
	if debug {
		httpOpts = append(httpOpts, httpx.WithDebug(log, "gitlab"))
	}
	httpClient := httpx.NewClient(httpOpts...)

	opts := []gl.ClientOptionFunc{
		gl.WithHTTPClient(httpClient),
	}
	if baseURL != "" {
		opts = append(opts, gl.WithBaseURL(baseURL))
	}

	as := &tokenRefresherAuthSource{refresher: refresher}
	glClient, err := gl.NewAuthSourceClient(as, opts...)
	if err != nil {
		return nil, fmt.Errorf("create GitLab client with refresher: %w", err)
	}

	return &Client{
		gl:  glClient,
		log: log,
	}, nil
}

// tokenRefresherAuthSource adapts a scm.TokenRefresher to the GitLab SDK's AuthSource interface.
// The SDK calls Header() on every request, so the token is always fresh.
type tokenRefresherAuthSource struct {
	refresher scm.TokenRefresher
}

func (a *tokenRefresherAuthSource) Init(_ context.Context, _ *gl.Client) error {
	return nil
}

func (a *tokenRefresherAuthSource) Header(ctx context.Context) (string, string, error) {
	tok, err := a.refresher.Token(ctx)
	if err != nil {
		return "", "", err
	}
	return "Authorization", "Bearer " + tok, nil
}
