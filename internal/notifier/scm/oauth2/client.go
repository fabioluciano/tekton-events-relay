// Package oauth2 provides OAuth2 token clients for the grants the relay can
// perform headlessly (no inbound redirect): client_credentials and refresh_token.
package oauth2

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// ClientCredentials holds OAuth2 client_credentials grant configuration.
type ClientCredentials struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
}

// RefreshTokenCredentials holds OAuth2 refresh_token grant configuration. The
// refresh token is obtained out of band (the interactive authorization_code
// flow is done elsewhere, since the relay exposes no redirect endpoint) and the
// resulting refresh token is provided here so the relay can mint and rotate
// access tokens headlessly.
type RefreshTokenCredentials struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
	RefreshToken string
}

// Client fetches OAuth2 access tokens via an x/oauth2 TokenSource.
// The token source automatically caches and refreshes tokens.
type Client struct {
	ts oauth2.TokenSource
}

// NewClient creates a client backed by the client_credentials grant.
// When httpClient is non-nil it is injected into the context via
// oauth2.HTTPClient so all token endpoint calls use that transport
// instead of http.DefaultClient.
func NewClient(creds ClientCredentials, httpClient *http.Client) *Client {
	cfg := clientcredentials.Config{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		TokenURL:     creds.TokenURL,
	}
	ctx := context.Background()
	if httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	}
	return &Client{ts: cfg.TokenSource(ctx)}
}

// NewRefreshTokenClient creates a client backed by the refresh_token grant.
// The x/oauth2 TokenSource exchanges the seeded refresh token for an access
// token and rotates it automatically before expiry.
// When httpClient is non-nil it is injected into the context via
// oauth2.HTTPClient so all token endpoint calls use that transport
// instead of http.DefaultClient.
func NewRefreshTokenClient(creds RefreshTokenCredentials, httpClient *http.Client) *Client {
	cfg := oauth2.Config{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		Endpoint:     oauth2.Endpoint{TokenURL: creds.TokenURL},
	}
	ctx := context.Background()
	if httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	}
	ts := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: creds.RefreshToken})
	return &Client{ts: ts}
}

// Token returns a valid access token, refreshing automatically as needed.
func (c *Client) Token(_ context.Context) (string, error) {
	tok, err := c.ts.Token()
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}
