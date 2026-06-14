package github

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gh "github.com/google/go-github/v68/github"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// HTTPDoer abstracts GitHub API HTTP operations for both token and app-based authentication.
type HTTPDoer interface {
	Do(ctx context.Context, method, url string, payload any) error
	DoWithResponse(ctx context.Context, method, url string, payload any, result any) error
	DoGraphQL(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error)
	BaseURL() string
	Token() string
}

// Client provides authenticated HTTP access to the GitHub REST and GraphQL APIs
// using the official go-github SDK.
type Client struct {
	gh      *gh.Client
	token   string
	baseURL string
	log     *zap.Logger
}

// NewClient creates a new GitHub API client with the given token, base URL, and TLS verification setting.
func NewClient(token, baseURL string, insecureSkipVerify bool, log *zap.Logger, debug bool) *Client {
	if baseURL == "" {
		baseURL = GitHubBaseURL
	}
	if log == nil {
		log = zap.NewNop()
	}

	httpClient := buildHTTPClient(insecureSkipVerify, debug, log)
	ghc := gh.NewClient(httpClient).WithAuthToken(token)

	if baseURL != GitHubBaseURL { //nolint:goconst
		var err error
		ghc, err = ghc.WithEnterpriseURLs(baseURL, baseURL+"/api/graphql")
		if err != nil {
			log.Warn("failed to configure enterprise URLs, using default",
				zap.String("baseURL", baseURL),
				zap.Error(err))
		}
	}

	return &Client{
		gh:      ghc,
		token:   token,
		baseURL: baseURL,
		log:     log,
	}
}

// buildHTTPClient creates an http.Client with appropriate TLS and timeout settings.
func buildHTTPClient(insecureSkipVerify bool, debug bool, log *zap.Logger) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = httpx.SharedMaxIdleConnsPerHost

	if insecureSkipVerify {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{} //nolint:gosec
		}
		transport.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec
	}

	var rt http.RoundTripper = transport
	if debug {
		rt = &debugRoundTripper{inner: transport, log: log}
	}

	return &http.Client{
		Transport: rt,
		Timeout:   30 * time.Second,
	}
}

// debugRoundTripper logs HTTP requests and responses.
type debugRoundTripper struct {
	inner http.RoundTripper
	log   *zap.Logger
}

func (d *debugRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	d.log.Debug("github HTTP request",
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()))
	resp, err := d.inner.RoundTrip(req)
	if err == nil {
		d.log.Debug("github HTTP response",
			zap.String("method", req.Method),
			zap.String("url", req.URL.String()),
			zap.Int("status", resp.StatusCode))
	}
	return resp, err
}

// GH returns the underlying go-github client for direct SDK usage.
func (c *Client) GH() *gh.Client {
	return c.gh
}

// Do performs an HTTP request with GitHub authentication and JSON encoding.
// Handlers use it to call endpoints not covered by the typed go-github client.
func (c *Client) Do(ctx context.Context, method, url string, payload any) error {
	return c.DoWithResponse(ctx, method, url, payload, nil)
}

// DoWithResponse performs an HTTP request and decodes the JSON response into result.
// Handlers use it to call endpoints not covered by the typed go-github client.
func (c *Client) DoWithResponse(ctx context.Context, method, url string, payload any, result any) error {
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			return fmt.Errorf("encode payload: %w", err)
		}
	}

	c.log.Debug("github HTTP request",
		zap.String("method", method),
		zap.String("url", url),
		zap.Int("payload_bytes", body.Len()))

	req, err := http.NewRequestWithContext(ctx, method, url, &body)
	if err != nil {
		return fmt.Errorf("create github request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	// Use the underlying http.Client which has auth transport (Bearer token)
	resp, err := c.gh.Client().Do(req)
	if err != nil {
		c.log.Error("github HTTP request failed",
			zap.String("method", method),
			zap.String("url", url),
			zap.Error(err))
		return fmt.Errorf("github http request to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	c.log.Debug("github HTTP response",
		zap.String("method", method),
		zap.String("url", url),
		zap.Int("status", resp.StatusCode))

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// DoGraphQL performs a GraphQL request to the GitHub API with authentication.
// Returns the parsed data field on success or detailed error from errors array.
func (c *Client) DoGraphQL(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	endpoint := c.graphqlEndpoint()

	reqBody := map[string]any{
		"query":     query,
		"variables": variables,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(reqBody); err != nil {
		return nil, fmt.Errorf("encode graphql payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, &buf)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", notifier.UserAgent)

	// Use the underlying http.Client which has auth transport (Bearer token)
	resp, err := c.gh.Client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github graphql API returned %d", resp.StatusCode)
	}

	var result struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphqlError  `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode graphql response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, formatGraphQLErrors(result.Errors)
	}

	return result.Data, nil
}

// graphqlEndpoint returns the GraphQL endpoint based on baseURL.
func (c *Client) graphqlEndpoint() string {
	if c.baseURL == GitHubBaseURL {
		return GitHubBaseURL + "/graphql"
	}
	// GHES: baseURL + /api/graphql
	return c.baseURL + "/api/graphql"
}

type graphqlError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Path    []any  `json:"path,omitempty"`
}

func formatGraphQLErrors(errors []graphqlError) error {
	if len(errors) == 1 {
		e := errors[0]
		msg := e.Message
		if e.Type != "" {
			msg = fmt.Sprintf("%s (type: %s)", msg, e.Type)
		}
		if len(e.Path) > 0 {
			msg = fmt.Sprintf("%s at path: %v", msg, e.Path)
		}
		return fmt.Errorf("graphql error: %s", msg)
	}

	// Multiple errors
	var b strings.Builder
	fmt.Fprintf(&b, "graphql returned %d errors: ", len(errors))
	for i, e := range errors {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(e.Message)
	}
	return fmt.Errorf("%s", b.String())
}

// BaseURL returns the GitHub API base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// Token returns the authentication token.
func (c *Client) Token() string {
	return c.token
}
