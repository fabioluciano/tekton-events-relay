package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
)

// applyAuth applies structured authentication based on the resolved auth.
// Returns an error if auth setup fails.
func applyAuth(req *http.Request, auth *ResolvedAuth) error {
	if auth == nil {
		return nil
	}

	switch auth.Type {
	case "bearer":
		return applyBearerAuth(req, auth)
	case "basic":
		return applyBasicAuth(req, auth)
	case "apikey":
		return applyAPIKeyAuth(req, auth)
	case "hmac":
		return applyHMACAuth(req, auth)
	default:
		return fmt.Errorf("unsupported auth type: %s", auth.Type)
	}
}

// applyBearerAuth sets the Authorization header with a Bearer token.
func applyBearerAuth(req *http.Request, auth *ResolvedAuth) error {
	req.Header.Set("Authorization", "Bearer "+auth.Token)
	return nil
}

// applyBasicAuth sets the Authorization header with Basic authentication.
func applyBasicAuth(req *http.Request, auth *ResolvedAuth) error {
	credentials := auth.Username + ":" + auth.Password
	encoded := base64.StdEncoding.EncodeToString([]byte(credentials))
	req.Header.Set("Authorization", "Basic "+encoded)
	return nil
}

// applyAPIKeyAuth sets a custom header with the API key.
func applyAPIKeyAuth(req *http.Request, auth *ResolvedAuth) error {
	req.Header.Set(auth.Header, auth.Token)
	return nil
}

// applyHMACAuth computes HMAC-SHA256 signature of the request body and sets the signature header.
// Pattern:
// 1. req.Body is created from bytes.NewReader (set at base.go:61)
// 2. Read all bytes from req.Body
// 3. Compute HMAC-SHA256(secret, bodyBytes)
// 4. Set signature header: X-Webhook-Signature: sha256=<hex>
// 5. Recreate body with a new bytes.Reader so it can be read again
func applyHMACAuth(req *http.Request, auth *ResolvedAuth) error {
	if req.Body == nil {
		return fmt.Errorf("request body is nil, cannot compute HMAC")
	}

	// Read the body bytes
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return fmt.Errorf("failed to read request body for HMAC: %w", err)
	}

	// Close the old body
	_ = req.Body.Close()

	// Compute HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(auth.Secret))
	if _, err := mac.Write(bodyBytes); err != nil {
		return fmt.Errorf("failed to compute HMAC: %w", err)
	}
	signature := hex.EncodeToString(mac.Sum(nil))

	// Set the signature header
	req.Header.Set("X-Webhook-Signature", "sha256="+signature)

	// Recreate the body so it can be read again during the actual request
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	return nil
}
