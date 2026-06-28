package github

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const pemRSAPrivateKey = "RSA PRIVATE KEY"

func TestGenerateAppJWT(t *testing.T) {
	// Generate test RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	// Write to temp file (simulates volume mount)
	tmpFile, err := os.CreateTemp("", "github-app-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  pemRSAPrivateKey,
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	if _, err := tmpFile.Write(privateKeyPEM); err != nil {
		t.Fatalf("Failed to write key: %v", err)
	}
	_ = tmpFile.Close()

	// Test JWT generation
	appID := int64(123456)
	jwtToken, err := generateAppJWTFromPath(appID, tmpFile.Name())
	if err != nil {
		t.Fatalf("generateAppJWTFromPath failed: %v", err)
	}

	// Verify JWT structure and claims
	token, err := jwt.ParseWithClaims(jwtToken, &jwt.RegisteredClaims{}, func(token *jwt.Token) (any, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			t.Errorf("Unexpected signing method: %v", token.Header["alg"])
		}
		return &privateKey.PublicKey, nil
	})

	if err != nil {
		t.Fatalf("Failed to parse JWT: %v", err)
	}

	if !token.Valid {
		t.Error("JWT token is not valid")
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		t.Fatal("Failed to cast claims")
	}

	// Verify issuer (app_id)
	if claims.Issuer != "123456" {
		t.Errorf("Expected issuer '123456', got '%s'", claims.Issuer)
	}

	// Verify expiry (should be ~10 minutes)
	expiryDuration := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	if expiryDuration < 9*time.Minute || expiryDuration > 11*time.Minute {
		t.Errorf("Expected expiry ~10 minutes, got %v", expiryDuration)
	}

	// Verify iat is recent
	if time.Since(claims.IssuedAt.Time) > 5*time.Second {
		t.Error("IssuedAt timestamp is not recent")
	}
}

func TestGenerateAppJWT_MissingFile(t *testing.T) {
	// Try to read from non-existent file
	_, err := generateAppJWTFromPath(123456, "/nonexistent/path/key.pem")
	if err == nil {
		t.Error("Expected error when key file does not exist, got nil")
	}
}

func TestGenerateAppJWT_InvalidKey(t *testing.T) {
	// Write invalid key to temp file
	tmpFile, err := os.CreateTemp("", "invalid-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	_, _ = tmpFile.WriteString("not a valid PEM key")
	_ = tmpFile.Close()

	_, err = generateAppJWTFromPath(123456, tmpFile.Name())
	if err == nil {
		t.Error("Expected error for invalid private key, got nil")
	}
}

func TestGenerateAppJWT_PKCS8Format(t *testing.T) {
	// Generate test RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	// Write PKCS8 key to temp file
	tmpFile, err := os.CreateTemp("", "github-app-pkcs8-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("Failed to marshal PKCS8: %v", err)
	}

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Bytes,
	})
	if _, err := tmpFile.Write(privateKeyPEM); err != nil {
		t.Fatalf("Failed to write key: %v", err)
	}
	_ = tmpFile.Close()

	// Test JWT generation with PKCS8 format
	appID := int64(789012)
	jwtToken, err := generateAppJWTFromPath(appID, tmpFile.Name())
	if err != nil {
		t.Fatalf("generateAppJWTFromPath failed with PKCS8: %v", err)
	}

	// Verify token can be parsed
	token, err := jwt.ParseWithClaims(jwtToken, &jwt.RegisteredClaims{}, func(_ *jwt.Token) (any, error) {
		return &privateKey.PublicKey, nil
	})

	if err != nil {
		t.Fatalf("Failed to parse JWT: %v", err)
	}

	if !token.Valid {
		t.Error("JWT token is not valid")
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		t.Fatal("Failed to cast claims")
	}

	if claims.Issuer != "789012" {
		t.Errorf("Expected issuer '789012', got '%s'", claims.Issuer)
	}
}
