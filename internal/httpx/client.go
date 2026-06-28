// Package httpx contains HTTP utilities for unified client behavior.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// SharedMaxIdleConnsPerHost is the default MaxIdleConnsPerHost for HTTP clients.
const SharedMaxIdleConnsPerHost = 100

type clientConfig struct {
	timeout            time.Duration
	debug              bool
	logger             *zap.Logger
	name               string
	insecureSkipVerify bool
}

// Option configures an HTTP client.
type Option func(*clientConfig)

// WithTimeout sets the HTTP client timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(c *clientConfig) {
		c.timeout = timeout
	}
}

// WithDebug enables debug logging for HTTP requests.
func WithDebug(logger *zap.Logger, name string) Option {
	return func(c *clientConfig) {
		c.debug = true
		c.logger = logger
		c.name = name
	}
}

// WithInsecureSkipVerify disables TLS certificate verification.
func WithInsecureSkipVerify() Option {
	return func(c *clientConfig) {
		c.insecureSkipVerify = true
	}
}

// NewClient creates a new HTTP client with connection pooling and optional configuration.
func NewClient(opts ...Option) *http.Client {
	cfg := &clientConfig{
		timeout: 30 * time.Second,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = SharedMaxIdleConnsPerHost
	transport.TLSClientConfig = TLSConfig(cfg.insecureSkipVerify)

	var finalTransport http.RoundTripper = transport

	if cfg.debug && cfg.logger != nil {
		finalTransport = &debugTransport{
			base:   finalTransport,
			logger: cfg.logger,
			name:   cfg.name,
		}
	}

	return &http.Client{
		Timeout:   cfg.timeout,
		Transport: finalTransport,
	}
}

// NewJSONRequest builds a request with a JSON-encoded body and content type.
func NewJSONRequest(ctx context.Context, method, url string, payload any) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode payload: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}
