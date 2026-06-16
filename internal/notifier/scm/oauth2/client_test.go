package oauth2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const (
	testClientSecret = "test-client-secret"
	jsonAccessToken  = "access_token"
)

func newTokenServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_Token_success(t *testing.T) {
	srv := newTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.FormValue("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %s", r.FormValue("grant_type"))
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "client-id" || pass != "client-secret" {
			t.Errorf("unexpected basic auth: user=%s ok=%v", user, ok)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			jsonAccessToken: "my-token",
			"expires_in":    3600,
		})
	})

	c := NewClient(ClientCredentials{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     srv.URL,
	}, nil)

	tok, err := c.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "my-token" {
		t.Errorf("expected 'my-token', got %q", tok)
	}
}

func TestClient_Token_cache_hit_no_second_request(t *testing.T) {
	var calls atomic.Int32
	srv := newTokenServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			jsonAccessToken: "cached-token",
			"expires_in":    3600,
		})
	})

	c := NewClient(ClientCredentials{
		ClientID:     "id",
		ClientSecret: testClientSecret,
		TokenURL:     srv.URL,
	}, nil)

	tok1, err := c.Token(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	tok2, err := c.Token(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if tok1 != tok2 {
		t.Errorf("expected same token, got %q and %q", tok1, tok2)
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 HTTP call, got %d", calls.Load())
	}
}

func TestRefreshTokenClient_Token_success(t *testing.T) {
	srv := newTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.FormValue("grant_type") != "refresh_token" {
			t.Errorf("expected grant_type=refresh_token, got %s", r.FormValue("grant_type"))
		}
		if r.FormValue("refresh_token") != "seeded-refresh" {
			t.Errorf("expected refresh_token=seeded-refresh, got %s", r.FormValue("refresh_token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			jsonAccessToken: "rotated-access",
			"expires_in":    3600,
		})
	})

	c := NewRefreshTokenClient(RefreshTokenCredentials{
		ClientID:     "id",
		ClientSecret: testClientSecret,
		TokenURL:     srv.URL,
		RefreshToken: "seeded-refresh",
	}, nil)

	tok, err := c.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "rotated-access" {
		t.Errorf("expected 'rotated-access', got %q", tok)
	}
}

func TestClient_Token_http_error(t *testing.T) {
	// Use a server that immediately closes the connection.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, _, _ := hj.Hijack()
		if err := conn.Close(); err != nil {
			t.Logf("close hijacked connection: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientCredentials{
		ClientID:     "id",
		ClientSecret: testClientSecret,
		TokenURL:     srv.URL,
	}, nil)

	_, err := c.Token(context.Background())
	if err == nil {
		t.Error("expected error for connection close, got nil")
	}
}

func TestClient_Token_non200_response(t *testing.T) {
	srv := newTokenServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})

	c := NewClient(ClientCredentials{
		ClientID:     "id",
		ClientSecret: testClientSecret,
		TokenURL:     srv.URL,
	}, nil)

	_, err := c.Token(context.Background())
	if err == nil {
		t.Error("expected error for 401, got nil")
	}
}
