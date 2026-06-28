package httpx

import (
	"crypto/tls"
	"errors"
	"net/http"
	"testing"
)

func TestTLSConfig_secure(t *testing.T) {
	cfg := TLSConfig(false)

	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want %x (TLS 1.2)", cfg.MinVersion, tls.VersionTLS12)
	}
	if cfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = true, want false when not explicitly requested")
	}
}

func TestTLSConfig_insecure(t *testing.T) {
	cfg := TLSConfig(true)

	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want %x (TLS 1.2)", cfg.MinVersion, tls.VersionTLS12)
	}
	if !cfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = false, want true when explicitly requested")
	}
}

func TestTLSRoundTripper_defaultTransport(t *testing.T) {
	rt := TLSRoundTripper(nil, false)

	transport, ok := rt.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", rt)
	}

	if transport.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig is nil")
	}
	if transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want %x", transport.TLSClientConfig.MinVersion, tls.VersionTLS12)
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = true, want false")
	}
}

func TestTLSRoundTripper_insecure(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	rt := TLSRoundTripper(base, true)

	transport, ok := rt.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", rt)
	}

	if transport.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig is nil")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = false, want true when explicitly requested")
	}
	if transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want %x", transport.TLSClientConfig.MinVersion, tls.VersionTLS12)
	}
}

func TestTLSRoundTripper_doesNotMutateBase(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	baseTLS := base.TLSClientConfig // may be nil

	_ = TLSRoundTripper(base, true)

	// The base transport must remain untouched.
	if baseTLS != base.TLSClientConfig {
		// If baseTLS was nil, the clone should still be nil.
		if baseTLS == nil && base.TLSClientConfig != nil {
			t.Error("base transport TLSClientConfig was mutated from nil to non-nil")
		}
	}
}

func TestTLSRoundTripper_nonTransportPassthrough(t *testing.T) {
	// A custom RoundTripper that is not *http.Transport should be returned as-is.
	custom := &stubRoundTripper{}
	rt := TLSRoundTripper(custom, true)

	if rt != custom {
		t.Error("expected the same custom RoundTripper instance back")
	}
}

// stubRoundTripper is a minimal http.RoundTripper for testing passthrough.
type stubRoundTripper struct{}

func (s *stubRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, errors.New("stubRoundTripper: RoundTrip unexpectedly called")
}
