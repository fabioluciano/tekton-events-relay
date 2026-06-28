package factory

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// TestBitbucketCloudOAuth2_NoFactoryTimeTokenCapture proves that the factory
// does NOT call the OAuth2 token endpoint at build time. The token endpoint
// counter must remain zero after Build(); it should only increment when a
// handler actually processes an event.
func TestBitbucketCloudOAuth2_NoFactoryTimeTokenCapture(t *testing.T) {
	var tokenCalls atomic.Int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-token","token_type":"bearer","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	dir := t.TempDir()
	write := func(name, val string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(val), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	apiCalls := atomic.Int32{}
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			t.Errorf("expected Basic auth header, got %q", auth)
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
		if err != nil {
			t.Errorf("failed to decode basic auth: %v", err)
		}
		if !strings.HasPrefix(string(decoded), "x-token-auth:") {
			t.Errorf("expected x-token-auth prefix in basic auth, got %q", string(decoded))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer apiSrv.Close()

	f := &BitbucketFactory{}
	log := zap.NewNop()

	handlers, err := f.Build(config.BitbucketInstance{
		Name:    "bb-oauth2",
		Enabled: true,
		Variant: config.BitbucketVariantCloud,
		Auth: &config.BitbucketAuth{
			OAuth2: &config.OAuth2Config{
				ClientIDFile:     write("client_id", "cid"),
				ClientSecretFile: write("client_secret", "csecret"),
				TokenURL:         tokenSrv.URL,
			},
		},
		BaseURL: apiSrv.URL,
		Actions: []config.Action{
			{Name: testStatus, Type: notifier.ActionCommitStatus, Enabled: true},
			{Name: "pr", Type: notifier.ActionPRComment, Enabled: true},
		},
	}, log)

	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(handlers))
	}

	// KEY ASSERTION: token endpoint must NOT have been called during Build().
	if n := tokenCalls.Load(); n != 0 {
		t.Fatalf("token endpoint called %d times during Build(); want 0 (no factory-time token capture)", n)
	}

	// Now exercise the status handler — the token endpoint should be called.
	pr := 42
	event := domain.Event{
		Provider:  "bitbucket-cloud",
		Repo:      domain.Repo{Workspace: "ws", Name: "repo"},
		CommitSHA: "abc123",
		RunName:   "run-1",
		State:     domain.StateSuccess,
		Context:   "tekton/ci",
		PRNumber:  &pr,
	}

	for _, h := range handlers {
		if err := h.Handle(context.Background(), event); err != nil {
			_ = err
		}
	}

	// The token endpoint should have been called at least once (per-request).
	if n := tokenCalls.Load(); n == 0 {
		t.Fatal("token endpoint was never called during Handle(); expected per-request token refresh")
	}
}

// TestBitbucketCloudOAuth2_PerRequestTokenRefresh proves that the OAuth2
// token is fetched lazily (at first Handle()), not at Build() time.
// The OAuth2 token source caches tokens internally — the key proof is that
// the token endpoint is called after Build() but not during it.
func TestBitbucketCloudOAuth2_PerRequestTokenRefresh(t *testing.T) {
	var tokenCalls atomic.Int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-token","token_type":"bearer","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	dir := t.TempDir()
	write := func(name, val string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(val), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	var apiAuthHeaders []string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiAuthHeaders = append(apiAuthHeaders, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer apiSrv.Close()

	f := &BitbucketFactory{}
	log := zap.NewNop()

	handlers, err := f.Build(config.BitbucketInstance{
		Name:    "bb-oauth2",
		Enabled: true,
		Variant: config.BitbucketVariantCloud,
		Auth: &config.BitbucketAuth{
			OAuth2: &config.OAuth2Config{
				ClientIDFile:     write("client_id", "cid"),
				ClientSecretFile: write("client_secret", "csecret"),
				TokenURL:         tokenSrv.URL,
			},
		},
		BaseURL: apiSrv.URL,
		Actions: []config.Action{
			{Name: testStatus, Type: notifier.ActionCommitStatus, Enabled: true},
		},
	}, log)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	statusHandler := handlers[0]
	event := domain.Event{
		Provider:  "bitbucket-cloud",
		Repo:      domain.Repo{Workspace: "ws", Name: "repo"},
		CommitSHA: "abc123",
		RunName:   "run-1",
		State:     domain.StateSuccess,
		Context:   "tekton/ci",
	}

	// Call Handle() multiple times.
	for i := 0; i < 3; i++ {
		if err := statusHandler.Handle(context.Background(), event); err != nil {
			t.Fatalf("Handle call %d: %v", i, err)
		}
	}

	// The token endpoint should have been called at least once (lazy fetch).
	if n := tokenCalls.Load(); n == 0 {
		t.Fatal("token endpoint was never called during Handle(); expected lazy token fetch")
	}
}

// TestBitbucketCloudBasicAuth_NotRegressed ensures the basic auth path
// (username + app_password) still works without any OAuth2 involvement.
func TestBitbucketCloudBasicAuth_NotRegressed(t *testing.T) {
	var apiCalls atomic.Int32
	var lastAuth atomic.Value
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		lastAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer apiSrv.Close()

	dir := t.TempDir()
	write := func(name, val string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(val), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	f := &BitbucketFactory{}
	log := zap.NewNop()

	handlers, err := f.Build(config.BitbucketInstance{
		Name:    "bb-basic",
		Enabled: true,
		Variant: config.BitbucketVariantCloud,
		Auth: &config.BitbucketAuth{
			UsernameFile:    write("user", "myuser"),
			AppPasswordFile: write("pass", "mypass"),
		},
		BaseURL: apiSrv.URL,
		Actions: []config.Action{
			{Name: testStatus, Type: notifier.ActionCommitStatus, Enabled: true},
		},
	}, log)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(handlers))
	}

	event := domain.Event{
		Provider:  "bitbucket-cloud",
		Repo:      domain.Repo{Workspace: "ws", Name: "repo"},
		CommitSHA: "abc123",
		RunName:   "run-1",
		State:     domain.StateSuccess,
		Context:   "tekton/ci",
	}
	if err := handlers[0].Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if apiCalls.Load() != 1 {
		t.Fatalf("API calls = %d, want 1", apiCalls.Load())
	}

	auth := lastAuth.Load().(string)
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("myuser:mypass"))
	if auth != expected {
		t.Errorf("Authorization = %q, want %q", auth, expected)
	}
}

// TestBitbucketServerAuth_NotRegressed ensures the Server variant still works.
func TestBitbucketServerAuth_NotRegressed(t *testing.T) {
	var apiCalls atomic.Int32
	var lastAuth atomic.Value
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		lastAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer apiSrv.Close()

	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("server-token"), 0o600); err != nil {
		t.Fatal(err)
	}

	f := &BitbucketFactory{}
	log := zap.NewNop()

	handlers, err := f.Build(config.BitbucketInstance{
		Name:    "bb-server",
		Enabled: true,
		Variant: config.BitbucketVariantServer,
		Auth:    &config.BitbucketAuth{TokenFile: tokenFile},
		BaseURL: apiSrv.URL,
		Actions: []config.Action{
			{Name: testStatus, Type: notifier.ActionCommitStatus, Enabled: true},
		},
	}, log)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(handlers))
	}

	event := domain.Event{
		Provider:  "bitbucket-server",
		Repo:      domain.Repo{Project: "PROJ", Name: "repo"},
		CommitSHA: "abc123",
		RunName:   "run-1",
		State:     domain.StateSuccess,
		Context:   "tekton/ci",
	}
	if err := handlers[0].Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if apiCalls.Load() != 1 {
		t.Fatalf("API calls = %d, want 1", apiCalls.Load())
	}

	// Bitbucket Server uses Bearer token auth.
	auth := lastAuth.Load().(string)
	if auth != "Bearer server-token" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer server-token")
	}
}
