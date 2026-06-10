// Package oauth2 provides an OAuth2 client_credentials token client.
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

// Client fetches OAuth2 tokens using the client_credentials grant via x/oauth2.
// The token source automatically caches and refreshes tokens.
type Client struct {
	ts oauth2.TokenSource
}

// NewClient creates an OAuth2 client backed by x/oauth2 TokenSource.
// The httpClient parameter is accepted for interface compatibility but the
// token source uses context.Background() for refresh requests.
func NewClient(creds ClientCredentials, _ *http.Client) *Client {
	cfg := clientcredentials.Config{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		TokenURL:     creds.TokenURL,
	}
	return &Client{ts: cfg.TokenSource(context.Background())}
}

// Token returns a valid access token, refreshing automatically as needed.
func (c *Client) Token(_ context.Context) (string, error) {
	tok, err := c.ts.Token()
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}
