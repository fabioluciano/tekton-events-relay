// Package bitbucket provides Bitbucket Cloud and Server SCM notifier clients.
package bitbucket

// No official Go SDK for Bitbucket Cloud as of 2026.
// github.com/ktrysmt/go-bitbucket exists but has intermittent maintenance;
// this client covers the 2 needed endpoints with minimal code.

import (
	"encoding/base64"
	"net/http"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// CloudClient holds shared HTTP client and auth for Bitbucket Cloud action handlers.
type CloudClient struct {
	*scm.BaseClient
}

// NewCloudClient creates a new Bitbucket Cloud API client.
func NewCloudClient(username, appPassword, baseURL string, insecureSkipVerify bool, debug bool, log *zap.Logger) *CloudClient {
	if baseURL == "" {
		baseURL = "https://api.bitbucket.org"
	}
	cred := base64.StdEncoding.EncodeToString([]byte(username + ":" + appPassword))
	return &CloudClient{
		BaseClient: scm.NewBaseClient(baseURL, insecureSkipVerify, debug, log, "bitbucket-cloud",
			func(r *http.Request) {
				r.Header.Set("Authorization", "Basic "+cred)
			}),
	}
}

// NewCloudClientWithAuth creates a new Bitbucket Cloud API client with a custom AuthFunc.
// Used for OAuth2 authentication where the auth function fetches a fresh token per request.
func NewCloudClientWithAuth(authFn scm.AuthFunc, baseURL string, insecureSkipVerify bool, debug bool, log *zap.Logger) *CloudClient {
	if baseURL == "" {
		baseURL = "https://api.bitbucket.org"
	}
	return &CloudClient{
		BaseClient: scm.NewBaseClient(baseURL, insecureSkipVerify, debug, log, "bitbucket-cloud", authFn),
	}
}
