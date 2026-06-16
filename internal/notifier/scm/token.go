package scm

import (
	"context"
	"fmt"
	"net/http"
)

// TokenRefresher provides a valid authentication token, refreshing automatically as needed.
// Implementations include OAuth2 clients (auto-refresh via x/oauth2 TokenSource),
// GitHub App clients (JWT → installation token), and static token wrappers (PATs).
type TokenRefresher interface {
	Token(ctx context.Context) (string, error)
}

// StaticToken wraps a fixed token string as a TokenRefresher.
// Used for PATs and other non-expiring credentials.
type StaticToken struct {
	token string
}

// NewStaticToken creates a StaticToken that always returns the given value.
func NewStaticToken(tok string) *StaticToken {
	return &StaticToken{token: tok}
}

// Token returns the static token value. It never refreshes.
func (s *StaticToken) Token(_ context.Context) (string, error) {
	return s.token, nil
}

// AuthStyle controls how the token is injected into HTTP requests.
type AuthStyle int

const (
	// AuthStyleBearer sets "Authorization: Bearer {token}" (OAuth2 standard).
	AuthStyleBearer AuthStyle = iota
	// AuthStyleToken sets "Authorization: token {token}" (Gitea convention).
	AuthStyleToken
	// AuthStyleHeader sets a custom header with the raw token value (e.g., GitLab's "PRIVATE-TOKEN").
	AuthStyleHeader
)

// TokenTransport is an http.RoundTripper that injects a fresh authentication
// token into every request via a TokenRefresher. It transparently handles
// token refresh for SDK-based providers (GitLab, Gitea) without recreating
// the SDK client or touching handler code.
type TokenTransport struct {
	Base      http.RoundTripper
	Refresher TokenRefresher
	Style     AuthStyle
	// HeaderName is used only with AuthStyleHeader (e.g., "PRIVATE-TOKEN" for GitLab).
	HeaderName string
}

// RoundTrip implements http.RoundTripper. It fetches a fresh token from the
// TokenRefresher and injects it into the request before delegating to the base transport.
func (t *TokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.Refresher.Token(req.Context())
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	req = req.Clone(req.Context())
	switch t.Style {
	case AuthStyleBearer:
		req.Header.Set("Authorization", "Bearer "+tok)
	case AuthStyleToken:
		req.Header.Set("Authorization", "token "+tok)
	case AuthStyleHeader:
		req.Header.Set(t.HeaderName, tok)
	}
	return t.Base.RoundTrip(req)
}
