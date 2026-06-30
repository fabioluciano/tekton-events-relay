package bitbucket

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// TestCloudClientWithAuth_PerRequestTokenRefresh proves that the AuthFunc
// supplied to NewCloudClientWithAuth is invoked on every HTTP request, not
// captured once at construction time.
func TestCloudClientWithAuth_PerRequestTokenRefresh(t *testing.T) {
	var authCalls atomic.Int32
	var lastAuth atomic.Value

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer apiSrv.Close()

	authFn := func(r *http.Request) {
		authCalls.Add(1)
		n := authCalls.Load()
		// Return a different token value each time to prove per-request resolution.
		tok := "token-v" + strings.Repeat("x", int(n))
		cred := base64.StdEncoding.EncodeToString([]byte("x-token-auth:" + tok))
		r.Header.Set("Authorization", "Basic "+cred)
	}

	client := NewCloudClientWithAuth(authFn, apiSrv.URL, false, false, zap.NewNop())
	reporter := NewCloudStatusReporterWithClient("bitbucket-cloud", client)

	event := domain.Event{
		Provider:  providerCloud,
		Repo:      domain.Repo{Workspace: "ws", Name: testRepoName},
		CommitSHA: "abc123",
		RunName:   testRunName,
		State:     domain.StateSuccess,
		Context:   testCIContext,
	}

	for i := 0; i < 3; i++ {
		if err := reporter.Handle(context.Background(), event); err != nil {
			t.Fatalf("Handle call %d: %v", i, err)
		}
	}

	if n := authCalls.Load(); n != 3 {
		t.Fatalf("AuthFunc called %d times after 3 Handle() calls; want 3 (per-request)", n)
	}
}

// TestCloudClientWithAuth_DifferentTokensPerRequest proves that when the
// AuthFunc returns different values, each HTTP request carries a different
// Authorization header — proving no token caching at the client level.
func TestCloudClientWithAuth_DifferentTokensPerRequest(t *testing.T) {
	var authHeaders []string

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer apiSrv.Close()

	var callCount atomic.Int32
	authFn := func(r *http.Request) {
		n := callCount.Add(1)
		tok := "rotating-token-" + string('A'+n-1)
		cred := base64.StdEncoding.EncodeToString([]byte("x-token-auth:" + tok))
		r.Header.Set("Authorization", "Basic "+cred)
	}

	client := NewCloudClientWithAuth(authFn, apiSrv.URL, false, false, zap.NewNop())
	reporter := NewCloudStatusReporterWithClient("bitbucket-cloud", client)

	event := domain.Event{
		Provider:  providerCloud,
		Repo:      domain.Repo{Workspace: "ws", Name: testRepoName},
		CommitSHA: "abc123",
		RunName:   testRunName,
		State:     domain.StateSuccess,
		Context:   testCIContext,
	}

	for i := 0; i < 3; i++ {
		if err := reporter.Handle(context.Background(), event); err != nil {
			t.Fatalf("Handle call %d: %v", i, err)
		}
	}

	if len(authHeaders) != 3 {
		t.Fatalf("got %d auth headers, want 3", len(authHeaders))
	}

	// Verify each header is different (proves rotating tokens).
	for i := 1; i < len(authHeaders); i++ {
		if authHeaders[i] == authHeaders[0] {
			t.Errorf("auth header %d is same as header 0: %q — expected different tokens per request", i, authHeaders[i])
		}
	}
}

// TestCloudCommentHandler_WithClientField proves CloudCommentConfig.Client
// is used when provided, bypassing Username/AppPassword.
func TestCloudCommentHandler_WithClientField(t *testing.T) {
	var authCalls atomic.Int32

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		authCalls.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer apiSrv.Close()

	authFn := func(r *http.Request) {
		authCalls.Add(1)
		tok := "per-request-token"
		cred := base64.StdEncoding.EncodeToString([]byte("x-token-auth:" + tok))
		r.Header.Set("Authorization", "Basic "+cred)
	}

	client := NewCloudClientWithAuth(authFn, apiSrv.URL, false, false, zap.NewNop())

	pr := 42
	handler, err := NewCloudCommentHandler(CloudCommentConfig{
		Client:  client,
		BaseURL: apiSrv.URL,
		Log:     zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewCloudCommentHandler: %v", err)
	}

	event := domain.Event{
		Provider: providerCloud,
		Repo:     domain.Repo{Workspace: "ws", Name: testRepoName},
		RunName:  testRunName,
		PRNumber: &pr,
		State:    domain.StateSuccess,
	}

	if err := handler.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if n := authCalls.Load(); n == 0 {
		t.Fatal("AuthFunc was never called; expected per-request auth when Client is provided")
	}
}

// TestNewCloudClient_BasicAuthStillWorks proves the basic auth constructor
// still produces correct Authorization headers (regression guard).
func TestNewCloudClient_BasicAuthStillWorks(t *testing.T) {
	var lastAuth atomic.Value

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer apiSrv.Close()

	client := NewCloudClient("myuser", "mypass", apiSrv.URL, false, false, zap.NewNop())
	reporter := NewCloudStatusReporterWithClient("bitbucket-cloud", client)

	event := domain.Event{
		Provider:  providerCloud,
		Repo:      domain.Repo{Workspace: "ws", Name: testRepoName},
		CommitSHA: "abc123",
		RunName:   testRunName,
		State:     domain.StateSuccess,
		Context:   testCIContext,
	}

	if err := reporter.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	auth := lastAuth.Load().(string)
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("myuser:mypass"))
	if auth != expected {
		t.Errorf("Authorization = %q, want %q", auth, expected)
	}
}
