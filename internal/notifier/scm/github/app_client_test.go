package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewAppClient(t *testing.T) {
	// Generate test private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	tmpFile, err := os.CreateTemp("", "github-app-key-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	_, _ = tmpFile.Write(privateKeyPEM)
	_ = tmpFile.Close()

	// Create mock server
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		//nolint:gosec,goconst // G101: test credential; "token" is a JSON key
		resp := map[string]any{
			"token":      "ghs_test_token",
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Mock generateAppJWT to use temp file
	originalGenJWT := generateAppJWT
	generateAppJWT = func(appID int64, _ string) (string, error) {
		return generateAppJWTFromPath(appID, tmpFile.Name())
	}
	defer func() { generateAppJWT = originalGenJWT }()

	// Create AppClient
	client, err := NewAppClient(123456, 789012, server.URL, false, zap.NewNop(), "")
	if err != nil {
		t.Fatalf("NewAppClient failed: %v", err)
	}

	if client.appID != 123456 {
		t.Errorf("Expected appID 123456, got %d", client.appID)
	}

	if client.installationID != 789012 {
		t.Errorf("Expected installationID 789012, got %d", client.installationID)
	}

	if client.currentToken == "" {
		t.Error("Expected token to be set after initialization")
	}

	if callCount != 1 {
		t.Errorf("Expected 1 token request during init, got %d", callCount)
	}
}

func TestAppClient_TokenRefresh(t *testing.T) {
	// Generate test private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	tmpFile, err := os.CreateTemp("", "github-app-key-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	_, _ = tmpFile.Write(privateKeyPEM)
	_ = tmpFile.Close()

	// Create mock server that issues tokens expiring in 1 second
	var tokenCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := tokenCount.Add(1)
		//nolint:gosec // G101: test credential
		resp := map[string]any{
			"token":      "ghs_token_" + strconv.Itoa(int(count)),
			"expires_at": time.Now().Add(1 * time.Second).Format(time.RFC3339),
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Mock generateAppJWT to use temp file
	originalGenJWT := generateAppJWT
	generateAppJWT = func(appID int64, _ string) (string, error) {
		return generateAppJWTFromPath(appID, tmpFile.Name())
	}
	defer func() { generateAppJWT = originalGenJWT }()

	// Create AppClient
	client, err := NewAppClient(123456, 789012, server.URL, false, zap.NewNop(), "")
	if err != nil {
		t.Fatalf("NewAppClient failed: %v", err)
	}

	firstToken, err := client.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() first call: %v", err)
	}

	time.Sleep(2 * time.Second)

	secondToken, err := client.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() second call: %v", err)
	}

	if firstToken == secondToken {
		t.Error("Expected token to be refreshed after expiry")
	}

	if tokenCount.Load() < 2 {
		t.Errorf("Expected at least 2 token requests (init + refresh), got %d", tokenCount.Load())
	}
}

func TestAppClient_Do(t *testing.T) {
	// Generate test private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	tmpFile, err := os.CreateTemp("", "github-app-key-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	_, _ = tmpFile.Write(privateKeyPEM)
	_ = tmpFile.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "access_tokens") {
			//nolint:gosec // G101: test credential
			resp := map[string]any{
				"token":      "ghs_app_token_123",
				"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	originalGenJWT := generateAppJWT
	generateAppJWT = func(appID int64, _ string) (string, error) {
		return generateAppJWTFromPath(appID, tmpFile.Name())
	}
	defer func() { generateAppJWT = originalGenJWT }()

	client, err := NewAppClient(123456, 789012, server.URL, false, zap.NewNop(), "")
	if err != nil {
		t.Fatalf("NewAppClient failed: %v", err)
	}

	token, err := client.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() failed: %v", err)
	}
	if !strings.HasPrefix(token, "ghs_app_token_") {
		t.Errorf("Expected installation token starting with ghs_app_token_, got: %s", token)
	}
}

func TestNewAppClient_InvalidPrivateKey(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "invalid-key-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	_, _ = tmpFile.WriteString("invalid key data")
	_ = tmpFile.Close()

	// Mock generateAppJWT to use temp file
	originalGenJWT := generateAppJWT
	generateAppJWT = func(appID int64, _ string) (string, error) {
		return generateAppJWTFromPath(appID, tmpFile.Name())
	}
	defer func() { generateAppJWT = originalGenJWT }()

	_, err = NewAppClient(123456, 789012, "https://api.github.com", false, zap.NewNop(), "")
	if err == nil {
		t.Error("Expected error for invalid private key, got nil")
	}
}
