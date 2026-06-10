// Package middleware provides HTTP middleware for authentication, rate limiting, and body limits.
package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AuthType represents supported authentication mechanisms.
type AuthType string

// AuthType constants define the supported authentication mechanisms.
const (
	AuthTypeHMACSHA256 AuthType = "hmac-sha256"
	AuthTypeBearer     AuthType = "bearer"
)

// Validator validates an incoming HTTP request against a secret.
type Validator interface {
	Validate(r *http.Request, secret string) bool
}

// ValidatorFunc is a function that implements Validator.
type ValidatorFunc func(r *http.Request, secret string) bool

// Validate implements the Validator interface.
func (f ValidatorFunc) Validate(r *http.Request, secret string) bool { return f(r, secret) }

var validators = map[AuthType]Validator{
	AuthTypeHMACSHA256: ValidatorFunc(validateHMAC),
	AuthTypeBearer:     ValidatorFunc(validateBearer),
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	Type   string
	Secret string
}

// AuthMiddleware returns HTTP middleware that validates requests using HMAC-SHA256 or Bearer token authentication.
func AuthMiddleware(config AuthConfig) (func(http.Handler) http.Handler, error) {
	v, ok := validators[AuthType(config.Type)]
	if !ok {
		return nil, fmt.Errorf("unsupported auth type: %s", config.Type)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !v.Validate(r, config.Secret) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func validateHMAC(r *http.Request, secret string) bool {
	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		return false
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

func validateBearer(r *http.Request, expectedToken string) bool {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return false
	}

	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return false
	}

	return hmac.Equal([]byte(parts[1]), []byte(expectedToken))
}
