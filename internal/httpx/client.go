// Package httpx contains HTTP utilities for unified client behavior.
package httpx

import (
	"crypto/tls"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// ClientConfig configures HTTP client behavior.
type ClientConfig struct {
	Timeout            time.Duration
	MaxRetries         int
	BaseDelay          time.Duration
	Debug              bool
	Logger             *zap.Logger
	Name               string // Provider/component name for debug logs (e.g. "github", "gitlab")
	InsecureSkipVerify bool   // Skip TLS certificate verification (unsafe, use only for self-signed certs)
}

// NewClient creates an *http.Client with the given configuration.
// If debug is enabled, attaches debug middleware via transport wrapper.
func NewClient(cfg ClientConfig) *http.Client {
	// Start with default transport, clone to avoid modifying global
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Configure TLS if InsecureSkipVerify is enabled
	if cfg.InsecureSkipVerify {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.InsecureSkipVerify = true
	}

	var finalTransport http.RoundTripper = transport

	// Wrap with debug if enabled
	if cfg.Debug && cfg.Logger != nil {
		finalTransport = &debugTransport{
			base:   finalTransport,
			logger: cfg.Logger,
			name:   cfg.Name,
		}
	}

	return &http.Client{
		Timeout:   cfg.Timeout,
		Transport: finalTransport,
	}
}

// DefaultClientConfig returns sensible defaults for HTTP client configuration.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		Timeout:    10 * time.Second,
		MaxRetries: 3,
		BaseDelay:  100 * time.Millisecond,
		Debug:      false,
		Logger:     nil,
	}
}
