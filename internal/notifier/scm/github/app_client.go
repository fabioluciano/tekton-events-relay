package github

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	gh "github.com/google/go-github/v68/github"
	"go.uber.org/zap"
)

const defaultPrivateKeyPath = "/etc/github-app/private-key.pem"

// AppClient wraps the base Client and manages GitHub App authentication.
// It automatically refreshes installation tokens when they expire.
type AppClient struct {
	*Client
	appID              int64
	installationID     int64
	privateKeyFile     string
	insecureSkipVerify bool
	debug              bool
	mu                 sync.RWMutex
	currentToken       string
	tokenExpiry        time.Time
}

// NewAppClient creates a new GitHub App client with automatic token management.
// privateKeyFile is the path to the RSA private key PEM file; empty string uses the default path.
func NewAppClient(appID, installationID int64, baseURL string, insecureSkipVerify bool, log *zap.Logger, privateKeyFile string) (*AppClient, error) {
	// Create base client with placeholder token - will be replaced immediately
	baseClient := NewClient("placeholder", baseURL, insecureSkipVerify, log, false)

	appClient := &AppClient{
		Client:             baseClient,
		appID:              appID,
		installationID:     installationID,
		privateKeyFile:     privateKeyFile,
		insecureSkipVerify: insecureSkipVerify,
		debug:              false,
	}

	// Get initial token - this will properly set up auth
	if err := appClient.refreshToken(context.Background()); err != nil {
		return nil, fmt.Errorf("initial token refresh: %w", err)
	}

	return appClient, nil
}

// refreshToken generates a new JWT and exchanges it for an installation token
// using the go-github SDK's Apps service.
func (a *AppClient) refreshToken(ctx context.Context) error {
	// Generate JWT
	jwtToken, err := generateAppJWT(a.appID, a.privateKeyFile)
	if err != nil {
		return fmt.Errorf("generate JWT: %w", err)
	}

	// Create a temporary client authenticated with the JWT to call the Apps API
	jwtClient := gh.NewClient(nil).WithAuthToken(jwtToken)
	if a.baseURL != "" && a.baseURL != "https://api.github.com" { //nolint:goconst
		jwtClient, err = jwtClient.WithEnterpriseURLs(a.baseURL, a.baseURL+"/api/graphql")
		if err != nil {
			return fmt.Errorf("configure enterprise URLs: %w", err)
		}
	}

	// Exchange JWT for installation token using the SDK
	token, _, err := jwtClient.Apps.CreateInstallationToken(ctx, a.installationID, nil)
	if err != nil {
		return fmt.Errorf("get installation token: %w", err)
	}

	// Update token with lock
	a.mu.Lock()
	a.currentToken = token.GetToken()
	a.tokenExpiry = token.GetExpiresAt().Time
	a.token = token.GetToken()
	// Rebuild the entire client stack to ensure fresh auth
	// (reusing old http.Client carries stale auth transport)
	freshBaseClient := NewClient(token.GetToken(), a.baseURL, a.insecureSkipVerify, a.log, a.debug)
	a.Client = freshBaseClient
	a.gh = freshBaseClient.gh
	a.mu.Unlock()

	a.log.Info("refreshed GitHub App installation token",
		zap.Int64("app_id", a.appID),
		zap.Int64("installation_id", a.installationID),
		zap.Time("expires_at", token.GetExpiresAt().Time))

	return nil
}

// generateAppJWT creates a JWT for GitHub App authentication.
// This is a package-level variable to allow mocking in tests.
var generateAppJWT = func(appID int64, keyPath string) (string, error) {
	if keyPath == "" {
		keyPath = defaultPrivateKeyPath
	}
	return generateAppJWTFromPath(appID, keyPath)
}

// generateAppJWTFromPath creates a JWT from a specific key path.
func generateAppJWTFromPath(appID int64, keyPath string) (string, error) {
	keyData, err := os.ReadFile(keyPath) //nolint:gosec // key path is configured by operator
	if err != nil {
		return "", fmt.Errorf("read private key from %s: %w", keyPath, err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return "", fmt.Errorf("failed to parse PEM block from private key")
	}

	var privateKey any
	if key, parseErr := x509.ParsePKCS1PrivateKey(block.Bytes); parseErr == nil {
		privateKey = key
	} else if key, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes); parseErr == nil {
		privateKey = key
	} else {
		return "", fmt.Errorf("failed to parse private key as PKCS1 or PKCS8: %w", parseErr)
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
		Issuer:    fmt.Sprintf("%d", appID),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	return signedToken, nil
}

// ensureValidToken checks if the current token is expired and refreshes if needed.
func (a *AppClient) ensureValidToken(ctx context.Context) error {
	a.mu.RLock()
	needsRefresh := time.Now().After(a.tokenExpiry.Add(-5 * time.Minute)) // Refresh 5min before expiry
	a.mu.RUnlock()

	if needsRefresh {
		return a.refreshToken(ctx)
	}
	return nil
}

// Do performs an HTTP request with automatic token refresh.
func (a *AppClient) Do(ctx context.Context, method, url string, payload any) error {
	if err := a.ensureValidToken(ctx); err != nil {
		return fmt.Errorf("ensure valid token: %w", err)
	}
	return a.Client.Do(ctx, method, url, payload)
}

// DoWithResponse performs an HTTP request with automatic token refresh and decodes the response.
func (a *AppClient) DoWithResponse(ctx context.Context, method, url string, payload any, result any) error {
	if err := a.ensureValidToken(ctx); err != nil {
		return fmt.Errorf("ensure valid token: %w", err)
	}
	return a.Client.DoWithResponse(ctx, method, url, payload, result)
}

// DoGraphQL performs a GraphQL request with automatic token refresh.
func (a *AppClient) DoGraphQL(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	if err := a.ensureValidToken(ctx); err != nil {
		return nil, fmt.Errorf("ensure valid token: %w", err)
	}
	return a.Client.DoGraphQL(ctx, query, variables)
}

// Token returns the current installation token (thread-safe).
// Used by factory when handlers haven't migrated to HTTPDoer interface yet.
func (a *AppClient) Token() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.currentToken
}

// BaseURL returns the GitHub API base URL (delegates to embedded Client).
func (a *AppClient) BaseURL() string {
	return a.Client.BaseURL()
}
