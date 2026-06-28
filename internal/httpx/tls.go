package httpx

import (
	"crypto/tls"
	"net/http"
)

// TLSConfig returns a *tls.Config with MinVersion set to TLS 1.2.
// When insecureSkipVerify is true, certificate verification is disabled.
// The insecureSkipVerify flag is always an explicit operator opt-in.
func TLSConfig(insecureSkipVerify bool) *tls.Config {
	//nolint:gosec // InsecureSkipVerify is set only when the operator explicitly opts in via config.
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecureSkipVerify,
	}
}

// TLSRoundTripper clones the given base transport and attaches a TLS
// configuration produced by TLSConfig. When base is nil,
// http.DefaultTransport is used as the starting point.
func TLSRoundTripper(base http.RoundTripper, insecureSkipVerify bool) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	transport, ok := base.(*http.Transport)
	if !ok {
		// If the base is not an *http.Transport (e.g. a custom wrapper),
		// return it as-is; the caller already controls TLS.
		return base
	}

	cloned := transport.Clone()
	cloned.TLSClientConfig = TLSConfig(insecureSkipVerify)
	return cloned
}
