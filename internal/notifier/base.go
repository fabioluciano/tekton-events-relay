package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

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
	AuthApplier func(req *http.Request)
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
}

func (b *Base) Send(ctx context.Context, e domain.Event) error {
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
		b.Auth(req)
	}

	resp, err := httpx.DoWithRetry(b.HTTP, req, 3, 500*time.Millisecond)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("responded %d: %s", resp.StatusCode, string(buf))
	}
	return nil
}

func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// NewBase returns a Base with sensible defaults for optional hooks.
func NewBase(client *http.Client, buildPayload PayloadBuilder, buildURL URLBuilder) *Base {
	b := &Base{
		HTTP:         client,
		BuildPayload: buildPayload,
		BuildURL:     buildURL,
		UserAgent:    UserAgent,
		Method:       func(_ domain.Event) string { return "POST" },
		Auth:         func(_ *http.Request) {},
	}
	return b
}

// ShouldNotify returns true if state matches any entry in notifyOn, or if notifyOn is empty.
func ShouldNotify(notifyOn []string, state domain.State) bool {
	if len(notifyOn) == 0 {
		return true
	}
	stateStr := string(state)
	for _, s := range notifyOn {
		if s == stateStr {
			return true
		}
	}
	return false
}
