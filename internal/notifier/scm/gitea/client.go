// Package gitea provides Gitea SCM integration for the relay.
package gitea

import (
	"fmt"
	"time"

	giteaSDK "code.gitea.io/sdk/gitea"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// Client holds the Gitea SDK client.
type Client struct {
	sdk *giteaSDK.Client
	log *zap.Logger
}

// NewClient creates a new Gitea API client using the official SDK.
func NewClient(token, baseURL string, insecureSkipVerify bool, debug bool, log *zap.Logger) (*Client, error) {
	if log == nil {
		log = zap.NewNop()
	}

	opts := []httpx.Option{httpx.WithTimeout(10 * time.Second)}
	if insecureSkipVerify {
		opts = append(opts, httpx.WithInsecureSkipVerify())
	}
	if debug {
		opts = append(opts, httpx.WithDebug(log, "gitea"))
	}
	httpClient := httpx.NewClient(opts...)

	c, err := giteaSDK.NewClient(baseURL,
		giteaSDK.SetToken(token),
		giteaSDK.SetHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("create gitea SDK client: %w", err)
	}

	return &Client{sdk: c, log: log}, nil
}

// NewClientWithRefresher creates a Gitea API client with automatic token refresh.
// The TokenRefresher provides a fresh token for every API request via a custom
// HTTP transport, transparently handling OAuth2 token expiry.
// The SDK's built-in token handling is bypassed (no SetToken call).
func NewClientWithRefresher(refresher scm.TokenRefresher, baseURL string, insecureSkipVerify bool, debug bool, log *zap.Logger) (*Client, error) {
	if log == nil {
		log = zap.NewNop()
	}

	opts := []httpx.Option{httpx.WithTimeout(10 * time.Second)}
	if insecureSkipVerify {
		opts = append(opts, httpx.WithInsecureSkipVerify())
	}
	if debug {
		opts = append(opts, httpx.WithDebug(log, "gitea"))
	}
	httpClient := httpx.NewClient(opts...)

	httpClient.Transport = &scm.TokenTransport{
		Base:      httpClient.Transport,
		Refresher: refresher,
		Style:     scm.AuthStyleToken,
	}

	c, err := giteaSDK.NewClient(baseURL,
		giteaSDK.SetHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("create gitea SDK client with refresher: %w", err)
	}

	return &Client{sdk: c, log: log}, nil
}
