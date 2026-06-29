package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	gh "github.com/google/go-github/v68/github"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// HTTPDoer abstracts GitHub API HTTP operations for both token and app-based authentication.
type HTTPDoer interface {
	DoGraphQL(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error)
	// GH returns the underlying go-github typed client for SDK-based API calls.
	GH() *gh.Client
}

// Client provides authenticated HTTP access to the GitHub REST and GraphQL APIs
// using the official go-github SDK.
type Client struct {
	gh      *gh.Client
	baseURL string
	log     *zap.Logger
}

// buildTransport creates an http.RoundTripper with appropriate TLS and optional debug settings.
func buildTransport(insecureSkipVerify bool, debug bool, log *zap.Logger) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = httpx.SharedMaxIdleConnsPerHost
	transport.TLSClientConfig = httpx.TLSConfig(insecureSkipVerify)

	var rt http.RoundTripper = transport
	if debug {
		rt = &debugRoundTripper{inner: transport, log: log}
	}
	return rt
}

// NewClientWithRefresher creates a GitHub API client that fetches a fresh token
// from the given TokenRefresher on every request via a scm.TokenTransport.
// No static token is stored; auth is injected per-request.
func NewClientWithRefresher(refresher scm.TokenRefresher, baseURL string, insecureSkipVerify bool, log *zap.Logger, debug bool) *Client {
	if baseURL == "" {
		baseURL = GitHubBaseURL
	}
	if log == nil {
		log = zap.NewNop()
	}

	rt := buildTransport(insecureSkipVerify, debug, log)
	tokenTransport := &scm.TokenTransport{
		Base:      rt,
		Refresher: refresher,
		Style:     scm.AuthStyleBearer,
	}
	httpClient := &http.Client{
		Transport: tokenTransport,
		Timeout:   30 * time.Second,
	}

	ghc := gh.NewClient(httpClient)
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
		baseURL: baseURL,
		log:     log,
	}
}

// NewClient creates a new GitHub API client with the given token, base URL, and TLS verification setting.
// Delegates to NewClientWithRefresher using a StaticToken so the token is injected via TokenTransport.
func NewClient(token, baseURL string, insecureSkipVerify bool, log *zap.Logger, debug bool) *Client {
	return NewClientWithRefresher(scm.NewStaticToken(token), baseURL, insecureSkipVerify, log, debug)
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
