package bitbucket

// No official Go SDK for Bitbucket Server/Data Center as of 2026.
// Manual HTTP client retained — only 2 endpoints needed.

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// ServerClient holds shared HTTP client and auth for Bitbucket Server action handlers.
type ServerClient struct {
	*scm.BaseClient
}

// NewServerClient creates a new Bitbucket Server API client.
func NewServerClient(token, baseURL string, insecureSkipVerify bool, debug bool, log *zap.Logger) *ServerClient {
	return &ServerClient{
		BaseClient: scm.NewBaseClient(baseURL, insecureSkipVerify, debug, log, "bitbucket-server",
			func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+token)
			}),
	}
}
