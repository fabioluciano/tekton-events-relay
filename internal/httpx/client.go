// Package httpx contains HTTP utilities for unified client behavior.
package httpx

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
)

type clientConfig struct {
	timeout            time.Duration
	debug              bool
	logger             *zap.Logger
	name               string
	insecureSkipVerify bool
	caBundleFile       string
	clientCertFile     string
	clientKeyFile      string
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

// WithCABundle trusts the given PEM bundle (in addition to nothing else):
// the safe alternative to InsecureSkipVerify for self-signed SCM instances.
func WithCABundle(path string) Option {
	return func(c *clientConfig) {
		c.caBundleFile = path
	}
}

// WithClientCertificate presents a client certificate (mTLS) to servers.
func WithClientCertificate(certFile, keyFile string) Option {
	return func(c *clientConfig) {
		c.clientCertFile = certFile
		c.clientKeyFile = keyFile
	}
}

// NewClient creates a new HTTP client with connection pooling and optional configuration.
func NewClient(opts ...Option) *http.Client {
	client, err := NewClientErr(opts...)
	if err != nil {
		// TLS material problems are configuration errors; surface them on
		// first use instead of panicking at build time.
		return &http.Client{Transport: errClientTransport{err: err}}
	}
	return client
}

// NewClientErr is NewClient with explicit error reporting for TLS material.
func NewClientErr(opts ...Option) (*http.Client, error) {
	cfg := &clientConfig{
		timeout: 30 * time.Second,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 100

	if cfg.insecureSkipVerify || cfg.caBundleFile != "" || cfg.clientCertFile != "" {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
		}
	}

	if cfg.insecureSkipVerify {
		transport.TLSClientConfig.InsecureSkipVerify = true
	}

	if cfg.caBundleFile != "" {
		pem, err := os.ReadFile(cfg.caBundleFile) // #nosec G304 -- path from validated config
		if err != nil {
			return nil, fmt.Errorf("read CA bundle: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA bundle %s contains no valid certificates", cfg.caBundleFile)
		}
		transport.TLSClientConfig.RootCAs = pool
	}

	if cfg.clientCertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.clientCertFile, cfg.clientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
	}

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
	}, nil
}

// errClientTransport fails every request with a fixed configuration error.
type errClientTransport struct{ err error }

func (t errClientTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}
