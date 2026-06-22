package github

import (
	"context"
	"crypto/x509"
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

// AppClient manages GitHub App authentication, automatically refreshing installation
// tokens when they expire. It implements scm.TokenRefresher so it can be passed to
// NewClientWithRefresher — the actual HTTP client is built separately.
type AppClient struct {
	appID              int64
	installationID     int64
	privateKeyFile     string
	insecureSkipVerify bool
	baseURL            string
	log                *zap.Logger
	mu                 sync.RWMutex
	currentToken       string
	tokenExpiry        time.Time
}

// NewAppClient creates a new GitHub App token manager and performs an initial token refresh.
// privateKeyFile is the path to the RSA private key PEM file; empty string uses the default path.
func NewAppClient(appID, installationID int64, baseURL string, insecureSkipVerify bool, log *zap.Logger, privateKeyFile string) (*AppClient, error) {
	if baseURL == "" {
		baseURL = GitHubBaseURL
	}
	if log == nil {
		log = zap.NewNop()
	}

	appClient := &AppClient{
		appID:              appID,
		installationID:     installationID,
		privateKeyFile:     privateKeyFile,
		insecureSkipVerify: insecureSkipVerify,
		baseURL:            baseURL,
		log:                log,
	}

	if err := appClient.refreshToken(context.Background()); err != nil {
		return nil, fmt.Errorf("initial token refresh: %w", err)
	}

	return appClient, nil
}

// Token returns a valid installation token, refreshing it automatically if it is
// within 5 minutes of expiry. Implements scm.TokenRefresher.
func (a *AppClient) Token(ctx context.Context) (string, error) {
	if err := a.ensureValidToken(ctx); err != nil {
		return "", fmt.Errorf("ensure valid token: %w", err)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.currentToken, nil
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
	if a.baseURL != "" && a.baseURL != GitHubBaseURL {
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

	// Update token under write lock — no client rebuild needed
	a.mu.Lock()
	a.currentToken = token.GetToken()
	a.tokenExpiry = token.GetExpiresAt().Time
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

	now := time.Now().UTC()
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
