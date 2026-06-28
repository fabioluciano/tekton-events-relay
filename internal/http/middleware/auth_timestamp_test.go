package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

//nolint:unparam // test helper accepts variable secret for flexibility
func signedRequest(t *testing.T, secret, body string, ts time.Time, withTimestamp bool) *http.Request {
	t.Helper()
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	if withTimestamp {
		req.Header.Set(TimestampHeader, strconv.FormatInt(ts.Unix(), 10))
	}
	return req
}

func runAuth(t *testing.T, cfg AuthConfig, req *http.Request) int {
	t.Helper()
	mw, err := AuthMiddleware(cfg)
	if err != nil {
		t.Fatalf("AuthMiddleware: %v", err)
	}
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)
	return rec.Code
}

func TestAuthTimestamp_FreshAccepted(t *testing.T) {
	cfg := AuthConfig{Type: "hmac-sha256", Secret: "s3cret", ValidateTimestamp: true} //nolint:goconst // test config
	req := signedRequest(t, "s3cret", "payload", time.Now(), true)
	if code := runAuth(t, cfg, req); code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
}

func TestAuthTimestamp_StaleRejected(t *testing.T) {
	cfg := AuthConfig{Type: "hmac-sha256", Secret: "s3cret", ValidateTimestamp: true}
	req := signedRequest(t, "s3cret", "payload", time.Now().Add(-10*time.Minute), true)
	if code := runAuth(t, cfg, req); code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (stale timestamp)", code)
	}
}

func TestAuthTimestamp_FutureBeyondToleranceRejected(t *testing.T) {
	cfg := AuthConfig{Type: "hmac-sha256", Secret: "s3cret", ValidateTimestamp: true}
	req := signedRequest(t, "s3cret", "payload", time.Now().Add(10*time.Minute), true)
	if code := runAuth(t, cfg, req); code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (future timestamp)", code)
	}
}

func TestAuthTimestamp_MissingHeaderRejected(t *testing.T) {
	cfg := AuthConfig{Type: "hmac-sha256", Secret: "s3cret", ValidateTimestamp: true}
	req := signedRequest(t, "s3cret", "payload", time.Now(), false)
	if code := runAuth(t, cfg, req); code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (missing timestamp header)", code)
	}
}

func TestAuthTimestamp_DisabledAcceptsSignedRequestWithoutTimestamp(t *testing.T) {
	// Given: replay protection is disabled on the lower-level middleware config.
	cfg := AuthConfig{Type: "hmac-sha256", Secret: "s3cret"}
	req := signedRequest(t, "s3cret", "payload", time.Now(), false)

	// When: a request has a valid HMAC but no replay timestamp.
	code := runAuth(t, cfg, req)

	// Then: the middleware preserves its existing direct-constructor behavior.
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200 (timestamp validation off)", code)
	}
}

func TestAuthTimestamp_DisabledIgnoresHeader(t *testing.T) {
	cfg := AuthConfig{Type: "hmac-sha256", Secret: "s3cret"}
	req := signedRequest(t, "s3cret", "payload", time.Now().Add(-time.Hour), true)
	if code := runAuth(t, cfg, req); code != http.StatusOK {
		t.Errorf("status = %d, want 200 (timestamp validation off)", code)
	}
}

func TestAuthTimestamp_CustomTolerance(t *testing.T) {
	cfg := AuthConfig{
		Type: "hmac-sha256", Secret: "s3cret",
		ValidateTimestamp: true, TimestampTolerance: time.Hour,
	}
	req := signedRequest(t, "s3cret", "payload", time.Now().Add(-30*time.Minute), true)
	if code := runAuth(t, cfg, req); code != http.StatusOK {
		t.Errorf("status = %d, want 200 (within custom tolerance)", code)
	}
}

func TestAuthTimestamp_GarbageHeaderRejected(t *testing.T) {
	cfg := AuthConfig{Type: "hmac-sha256", Secret: "s3cret", ValidateTimestamp: true}
	req := signedRequest(t, "s3cret", "payload", time.Now(), false)
	req.Header.Set(TimestampHeader, "not-a-number")
	if code := runAuth(t, cfg, req); code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (unparsable timestamp)", code)
	}
}
