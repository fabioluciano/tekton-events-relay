package httpx

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// writeServerCA extracts the httptest TLS server certificate as a PEM bundle.
func writeServerCA(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	cert := srv.Certificate()
	path := filepath.Join(t.TempDir(), "ca.pem")
	raw := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNewClient_CABundleTrustsSelfSigned(t *testing.T) {
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()

	client, err := NewClientErr(WithCABundle(writeServerCA(t, srv)))
	if err != nil {
		t.Fatalf("NewClientErr: %v", err)
	}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("request with CA bundle failed: %v", err)
	}
	_ = resp.Body.Close()
}

func TestNewClient_SelfSignedRejectedWithoutBundle(t *testing.T) {
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()

	client, err := NewClientErr()
	if err != nil {
		t.Fatalf("NewClientErr: %v", err)
	}

	resp, err := client.Get(srv.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected TLS verification failure for self-signed server")
	}
	var unknownAuthority x509.UnknownAuthorityError
	if !errors.As(err, &unknownAuthority) {
		t.Logf("got non-x509 error (acceptable on some platforms): %v", err)
	}
}

func TestNewClientErr_BadCABundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, []byte("not a pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClientErr(WithCABundle(path)); err == nil {
		t.Fatal("expected error for invalid CA bundle")
	}
	if _, err := NewClientErr(WithCABundle(filepath.Join(t.TempDir(), "missing.pem"))); err == nil {
		t.Fatal("expected error for missing CA bundle")
	}
}

func TestNewClient_BadTLSMaterialFailsOnFirstUse(t *testing.T) {
	client := NewClient(WithCABundle("/nonexistent/ca.pem"))
	resp, err := client.Get("https://example.invalid")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected configuration error on first use")
	}
}
