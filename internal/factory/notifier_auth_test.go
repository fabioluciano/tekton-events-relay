package factory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
)

func TestResolveBearerRefresher_FileReReadsOnRotation(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := resolveBearerRefresher(nil, tokenFile, "", "grafana", "prod", zap.NewNop())
	if err != nil {
		t.Fatalf("resolveBearerRefresher: %v", err)
	}

	got, err := r.Token(context.Background())
	if err != nil || got != "v1" {
		t.Fatalf("Token() = %q, %v; want v1", got, err)
	}

	if err := os.WriteFile(tokenFile, []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = r.Token(context.Background())
	if err != nil || got != "v2" {
		t.Fatalf("Token() after rotation = %q, %v; want v2", got, err)
	}
}

func TestResolveBearerRefresher_MissingFileFailsFast(t *testing.T) {
	_, err := resolveBearerRefresher(nil, filepath.Join(t.TempDir(), "nope"), "", "sentry", "prod", zap.NewNop())
	if err == nil {
		t.Fatal("expected fail-fast error for missing token file")
	}
}

func TestResolveBearerRefresher_OAuth2FetchesToken(t *testing.T) {
	var tokenCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"oauth-abc","token_type":"bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	idFile := filepath.Join(dir, "client_id")
	secretFile := filepath.Join(dir, "client_secret")
	if err := os.WriteFile(idFile, []byte("cid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretFile, []byte("csecret"), 0o600); err != nil {
		t.Fatal(err)
	}

	oauth2cfg := &config.OAuth2Config{
		ClientIDFile:     idFile,
		ClientSecretFile: secretFile,
		TokenURL:         srv.URL,
	}

	r, err := resolveBearerRefresher(oauth2cfg, "", "", "webhook", "prod", zap.NewNop())
	if err != nil {
		t.Fatalf("resolveBearerRefresher: %v", err)
	}

	tok, err := r.Token(context.Background())
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	if tok != "oauth-abc" {
		t.Fatalf("Token() = %q, want oauth-abc", tok)
	}

	// The cached TokenSource should not hit the token endpoint again while valid.
	if _, err := r.Token(context.Background()); err != nil {
		t.Fatalf("Token() second call: %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("token endpoint called %d times, want 1 (token should be cached)", tokenCalls)
	}
}

func TestResolveOAuth2Refresher_RefreshTokenGrant(t *testing.T) {
	var gotGrant, gotRefresh string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotGrant = r.FormValue("grant_type")
		gotRefresh = r.FormValue("refresh_token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"rt-access","token_type":"bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	write := func(name, val string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(val), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cfg := &config.OAuth2Config{
		GrantType:        config.OAuth2GrantRefreshToken,
		ClientIDFile:     write("client_id", "cid"),
		ClientSecretFile: write("client_secret", "csecret"),
		RefreshTokenFile: write("refresh_token", "seed-rt"),
		TokenURL:         srv.URL,
	}

	r, err := resolveOAuth2Refresher(cfg, "webhook", "prod", zap.NewNop())
	if err != nil {
		t.Fatalf("resolveOAuth2Refresher: %v", err)
	}
	tok, err := r.Token(context.Background())
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	if tok != "rt-access" {
		t.Fatalf("Token() = %q, want rt-access", tok)
	}
	if gotGrant != "refresh_token" || gotRefresh != "seed-rt" {
		t.Fatalf("token request used grant=%q refresh_token=%q; want refresh_token/seed-rt", gotGrant, gotRefresh)
	}
}

func TestResolveOAuth2Refresher_InvalidGrant(t *testing.T) {
	dir := t.TempDir()
	write := func(name, val string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(val), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg := &config.OAuth2Config{ //nolint:gosec // G101: test fixture file paths, not credentials
		GrantType:        "authorization_code",
		ClientIDFile:     write("client_id", "cid"),
		ClientSecretFile: write("client_secret", "csecret"),
		TokenURL:         "https://x/token",
	}
	_, err := resolveOAuth2Refresher(cfg, "webhook", "prod", zap.NewNop())
	if err == nil {
		t.Fatal("expected error for unsupported grant_type")
	}
	if !strings.Contains(err.Error(), "grant_type") {
		t.Fatalf("error should mention grant_type, got %v", err)
	}
}
