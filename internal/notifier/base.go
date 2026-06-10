package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
)

// UserAgent is the User-Agent header value used by all notifiers.
const UserAgent = "tekton-events-relay"

// Template Method hooks — each concrete notifier provides the ones it needs.
type (
	// PayloadBuilder constructs the provider-specific payload from a domain event.
	PayloadBuilder func(domain.Event) (any, error)
	// URLBuilder constructs the provider-specific URL from a domain event.
	URLBuilder func(domain.Event) (string, error)
	// AuthApplier applies authentication to the HTTP request.
	AuthApplier func(req *http.Request) error
	// MethodSelector determines the HTTP method for the request.
	MethodSelector func(domain.Event) string
)

// Base encapsulates the common HTTP send flow with retry.
type Base struct {
	HTTP         *http.Client
	BuildPayload PayloadBuilder
	BuildURL     URLBuilder
	Auth         AuthApplier
	Method       MethodSelector
	UserAgent    string
	Log          *zap.Logger
}

// Send executes the common HTTP send flow with retry.
func (b *Base) Send(ctx context.Context, e domain.Event) error {
	start := time.Now()

	url, err := b.BuildURL(e)
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}
	payload, err := b.BuildPayload(e)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	method := http.MethodPost
	if b.Method != nil {
		method = b.Method(e)
	}

	// Log delivery attempt
	if b.Log != nil {
		b.Log.Info("webhook_delivery_started",
			zap.String("url", url),
			zap.String("method", method),
			zap.Int("payload_bytes", len(body)),
			zap.String("event_run_id", e.RunID),
			zap.String("event_resource", string(e.Resource)),
			zap.String("event_state", string(e.State)),
		)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if b.UserAgent != "" {
		req.Header.Set("User-Agent", b.UserAgent)
	}
	if b.Auth != nil {
		if err := b.Auth(req); err != nil {
			if b.Log != nil {
				b.Log.Error("webhook_auth_failed",
					zap.String("url", url),
					zap.Error(err),
				)
			}
			return fmt.Errorf("apply auth: %w", err)
		}
	}

	resp, err := httpx.DoWithRetryPolicy(b.HTTP, req, httpx.DefaultRetryPolicy())
	duration := time.Since(start)

	if err != nil {
		if b.Log != nil {
			b.Log.Error("webhook_delivery_failed",
				zap.String("url", url),
				zap.String("method", method),
				zap.Int64("duration_ms", duration.Milliseconds()),
				zap.String("event_run_id", e.RunID),
				zap.Error(err),
			)
		}
		return fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

		// Log with appropriate severity
		if b.Log != nil {
			level := zap.WarnLevel
			if resp.StatusCode >= 500 {
				level = zap.ErrorLevel
			}

			b.Log.Log(level, "webhook_delivery_error",
				zap.String("url", url),
				zap.String("method", method),
				zap.Int("status", resp.StatusCode),
				zap.String("response_body", string(buf)),
				zap.Int64("duration_ms", duration.Milliseconds()),
				zap.String("event_run_id", e.RunID),
			)
		}

		return fmt.Errorf("responded %d: %s", resp.StatusCode, string(buf))
	}

	// Log success
	if b.Log != nil {
		b.Log.Info("webhook_delivery_success",
			zap.String("url", url),
			zap.String("method", method),
			zap.Int("status", resp.StatusCode),
			zap.Int64("duration_ms", duration.Milliseconds()),
			zap.String("event_run_id", e.RunID),
		)
	}

	return nil
}

// DefaultHTTPClient returns an HTTP client with sensible defaults for notifiers.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}
