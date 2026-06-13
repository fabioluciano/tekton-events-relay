// Package gitea provides Gitea SCM integration for the relay.
package gitea

import (
	"fmt"
	"time"

	giteaSDK "code.gitea.io/sdk/gitea"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
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
