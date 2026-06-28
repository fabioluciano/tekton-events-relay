// Package config provides configuration loading, validation, and hot-reload
// for the tekton-events-relay binary.
package config

import (
	"fmt"
	"net"
	"net/url"
	"time"
)

// Server, auth, store, retry, limits, and tracing validators.

func (c *Config) validateServer() error {
	if c.MaxConcurrency != 0 && (c.MaxConcurrency < 1 || c.MaxConcurrency > 500) {
		return fmt.Errorf("max_concurrency: must be between 1 and 500 (or 0 for default), got %d", c.MaxConcurrency)
	}
	if c.Server.Auth.Enabled {
		switch c.Server.Auth.Type {
		case AuthTypeHMACSHA256, AuthTypeBearer:
		default:
			return fmt.Errorf("server.auth.type: unsupported auth type '%s' (must be 'hmac-sha256' or 'bearer')", c.Server.Auth.Type)
		}
		if c.Server.Auth.SecretFile == "" {
			return fmt.Errorf("server.auth: enabled but missing 'secret_file'")
		}
		if c.Server.Auth.Type == AuthTypeHMACSHA256 && !c.Server.Auth.ValidateTimestamp {
			return fmt.Errorf("server.auth.validate_timestamp: %s", ValidationMsgHMACReplayRequired)
		}
	}
	return nil
}

func (c *Config) validateStore() error {
	switch c.Store.Backend {
	case "", "memory", "olric":
	case "valkey":
		if c.Store.Valkey.Address == "" {
			return fmt.Errorf("store.valkey: backend selected but missing 'address'")
		}
	default:
		return fmt.Errorf("store.backend: unsupported backend '%s' (must be 'memory', 'valkey' or 'olric')", c.Store.Backend)
	}
	for i, peer := range c.Store.Olric.Peers {
		host, port, err := net.SplitHostPort(peer)
		if err != nil || host == "" || port == "" {
			return fmt.Errorf("store.olric.peers[%d]: '%s' must be in host:port format", i, peer)
		}
	}
	return nil
}

func (c *Config) validateRetry() error {
	if c.Retry.MaxAttempts < 0 {
		return fmt.Errorf("retry.max_attempts: must be non-negative")
	}
	if c.Retry.InitialBackoff < 0 || c.Retry.MaxBackoff < 0 {
		return fmt.Errorf("retry: backoff values must be non-negative")
	}
	if c.Retry.InitialBackoff > c.Retry.MaxBackoff && c.Retry.MaxBackoff > 0 {
		return fmt.Errorf("retry: initial_backoff must be <= max_backoff")
	}
	return nil
}

func (c *Config) validateLimits() error {
	if c.DedupeSize > 1000000 {
		return fmt.Errorf("dedupe_size: maximum is 1000000")
	}
	if c.DLQ.Enabled && c.DLQ.MaxSizeBytes < 1024 {
		return fmt.Errorf("dlq.max_size_bytes: minimum is 1024")
	}
	if c.HandlerTimeout > 0 && c.Server.WriteTimeoutSec > 0 && c.HandlerTimeout > time.Duration(c.Server.WriteTimeoutSec)*time.Second {
		return fmt.Errorf("handler_timeout must be less than write_timeout")
	}
	return nil
}

func (c *Config) validateTracing() error {
	if c.Tracing.Endpoint != "" {
		if _, err := url.ParseRequestURI(c.Tracing.Endpoint); err != nil {
			return fmt.Errorf("tracing.endpoint: invalid URL '%s': %w", c.Tracing.Endpoint, err)
		}
	}
	return nil
}

func (c *Config) validateTLS() error {
	if c.Server.TLS.CertFile != "" && c.Server.TLS.KeyFile == "" {
		return fmt.Errorf("server.tls: cert_file set but missing key_file")
	}
	if c.Server.TLS.KeyFile != "" && c.Server.TLS.CertFile == "" {
		return fmt.Errorf("server.tls: key_file set but missing cert_file")
	}
	return nil
}

func (c *Config) validateLogging() error {
	if (c.Logging.Verbose.Caller || c.Logging.Verbose.HTTPCalls || c.Logging.Verbose.Payloads) && c.Logging.Level != "debug" {
		return fmt.Errorf("logging.verbose: verbose options (caller, http_calls, payloads) require logging.level to be 'debug', current level is '%s'", c.Logging.Level)
	}
	return nil
}

// checkDuplicateName is a helper for duplicate instance name checks across provider types.
func checkDuplicateName(providerPath string, name string, names map[string]map[string]bool) error {
	if names[providerPath] == nil {
		names[providerPath] = make(map[string]bool)
	}
	if names[providerPath][name] {
		return fmt.Errorf("%s: duplicate instance name '%s'", providerPath, name)
	}
	names[providerPath][name] = true
	return nil
}
