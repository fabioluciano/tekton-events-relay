package scm

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
)

// BaseClient holds shared HTTP configuration for SCM providers that use DoJSON.
// GitHub is excluded — it uses the go-github SDK directly.
type BaseClient struct {
	HTTP    *http.Client
	AuthFn  AuthFunc
	BaseURL string
	Retry   httpx.RetryPolicy
}

// NewBaseClient builds a BaseClient with optional TLS and debug transport.
func NewBaseClient(baseURL string, insecureSkipVerify bool, debug bool, log *zap.Logger, provider string, authFn AuthFunc) *BaseClient {
	if log == nil {
		log = zap.NewNop()
	}
	opts := []httpx.Option{httpx.WithTimeout(10 * time.Second)}
	if insecureSkipVerify {
		opts = append(opts, httpx.WithInsecureSkipVerify())
	}
	if debug {
		opts = append(opts, httpx.WithDebug(log, provider))
	}
	return &BaseClient{
		HTTP:    httpx.NewClient(opts...),
		AuthFn:  authFn,
		BaseURL: baseURL,
		Retry:   httpx.DefaultRetryPolicy(),
	}
}

// Do performs an authenticated HTTP request without decoding the response.
func (b *BaseClient) Do(ctx context.Context, method, url string, payload any) error {
	return DoJSON(ctx, b.HTTP, b.Retry, method, url, payload, b.AuthFn, nil)
}

// DoWithResponse performs an authenticated HTTP request and decodes the response into v.
func (b *BaseClient) DoWithResponse(ctx context.Context, method, url string, payload any, v any) error {
	return DoJSON(ctx, b.HTTP, b.Retry, method, url, payload, b.AuthFn, v)
}
