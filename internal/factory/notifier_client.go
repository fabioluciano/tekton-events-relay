package factory

import (
	"net/http"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
)

// buildNotifierClient returns an HTTP client and optional retry policy for a
// notifier instance. When ro is non-nil, a dedicated client with connection
// pooling is created via httpx.NewClient and the retry policy fields are
// carried (zero fields fall back to the global defaults via normalized() at
// DoWithRetryPolicy time). When ro is nil, both returns are nil — the notifier
// uses DefaultHTTPClient and the global retry policy.
func buildNotifierClient(ro *config.RetryOverride) (*http.Client, *httpx.RetryPolicy) {
	if ro == nil {
		return nil, nil
	}
	// Build a dedicated HTTP client with connection pooling (30s timeout,
	// shared idle connections) for this notifier instance.
	httpClient := httpx.NewClient()

	rp := &httpx.RetryPolicy{
		MaxAttempts:    ro.MaxAttempts,
		InitialBackoff: ro.InitialBackoff,
		MaxBackoff:     ro.MaxBackoff,
	}
	// Unset fields stay zero; normalized() in DoWithRetryPolicy fills
	// the zero fields with global defaults.
	return httpClient, rp
}
