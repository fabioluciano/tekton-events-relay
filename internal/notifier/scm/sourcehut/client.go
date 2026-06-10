// Package sourcehut provides SourceHut SCM notifier client.
package sourcehut

// No official Go SDK for SourceHut as of 2026.
// The SourceHut builds API is unique (jobs API for statuses). Manual HTTP client retained.

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// Client holds shared HTTP client and auth for all SourceHut action handlers.
type Client struct {
	*scm.BaseClient
}

// NewClient creates a new SourceHut API client with the given token and base URL.
func NewClient(token, baseURL string, insecureSkipVerify bool, debug bool, log *zap.Logger) *Client {
	if baseURL == "" {
		baseURL = "https://builds.sr.ht"
	}

	return &Client{
		BaseClient: scm.NewBaseClient(baseURL, insecureSkipVerify, debug, log, "sourcehut",
			func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+token)
			}),
	}
}
