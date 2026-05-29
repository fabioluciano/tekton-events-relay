package bitbucket

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	testCommitAbc123           = "testCommitAbc123"
	testWorkspace              = "testWorkspace"
	testRepo                   = "testRepo"
	testContext                = "test-context"
	testDescription            = "Test description"
	testExampleURL             = "https://example.com"
	testBuildContext           = "build/test"
	testCIURL                  = "https://ci.example.com"
	testRunID123               = "run-123"
	testToken                  = "testToken"
	testBitbucketEnterpriseURL = "testBitbucketEnterpriseURL"
	testHello                  = "hello"
)

// ============================================================
//   Bitbucket Cloud Tests
// ============================================================

func TestNewCloud(t *testing.T) {
	cfg := CloudConfig{
		Username:    "testuser",
		AppPassword: "testpass",
		BaseURL:     bitbucketAPIBaseURL,
	}

	reporter := NewCloud(cfg)

	if reporter == nil {
		t.Fatal("NewCloud returned nil")
	}
	if reporter.cfg.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %s", reporter.cfg.Username)
	}
	if reporter.cfg.AppPassword != "testpass" {
		t.Errorf("expected app password 'testpass', got %s", reporter.cfg.AppPassword)
	}
	if reporter.base == nil {
		t.Fatal("base notifier not initialized")
	}
	if reporter.base.UserAgent != notifier.UserAgent {
		t.Errorf("expected user agent 'notifier.UserAgent', got %s", reporter.base.UserAgent)
	}
}

func TestCloudReporter_Name(t *testing.T) {
	reporter := NewCloud(CloudConfig{})
	if name := reporter.Name(); name != "bitbucket-cloud" {
		t.Errorf("expected name 'bitbucket-cloud', got %s", name)
	}
}

func TestCloudReporter_Notify_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify HTTP method
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify auth header
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			t.Errorf("expected Basic auth, got %s", auth)
		}

		// Verify content type
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected content-type application/json, got %s", ct)
		}

		// Verify user agent
		if ua := r.Header.Get("User-Agent"); ua != notifier.UserAgent {
			t.Errorf("expected user-agent notifier.UserAgent, got %s", ua)
		}

		// Verify URL path
		expectedPath := "/2.0/repositories/testWorkspace/testRepo/commit/testCommitAbc123/statuses/build"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}

		// Parse and verify payload
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}

		if payload["state"] != stateSuccessful {
			t.Errorf("expected state stateSuccessful, got %s", payload["state"])
		}
		if payload["key"] != testContext {
			t.Errorf("expected key testContext, got %s", payload["key"])
		}
		if payload["name"] != testContext {
			t.Errorf("expected name testContext, got %s", payload["name"])
		}
		if payload["description"] != testDescription {
			t.Errorf("expected description 'testDescription', got %s", payload["description"])
		}
		if payload["url"] != testExampleURL {
			t.Errorf("expected url testExampleURL, got %s", payload["url"])
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	reporter := NewCloud(CloudConfig{
		Username:    "user",
		AppPassword: "pass",
		BaseURL:     server.URL,
	})

	event := domain.Event{
		State:       domain.StateSuccess,
		Context:     testContext,
		Description: testDescription,
		TargetURL:   testExampleURL,
		CommitSHA:   testCommitAbc123,
		Repo: domain.Repo{
			Workspace: testWorkspace,
			Name:      testRepo,
		},
	}

	err := reporter.Notify(context.Background(), event)
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}
}

func TestCloudReporter_Notify_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	reporter := NewCloud(CloudConfig{
		Username:    "user",
		AppPassword: "wrongpass",
		BaseURL:     server.URL,
	})

	event := domain.Event{
		State:     domain.StateSuccess,
		CommitSHA: testCommitAbc123,
		Repo: domain.Repo{
			Workspace: testWorkspace,
			Name:      testRepo,
		},
	}

	err := reporter.Notify(context.Background(), event)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}

func TestMapCloudState(t *testing.T) {
	tests := []struct {
		input    domain.State
		expected string
	}{
		{domain.StateSuccess, stateSuccessful},
		{domain.StateFailure, stateFailed},
		{domain.StateError, stateFailed},
		{domain.StateCanceled, "STOPPED"},
		{domain.StatePending, stateInProgress},
		{domain.StateRunning, stateInProgress},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := bitbucketCloudStateMapLegacy.Map(tt.input, stateInProgress)
			if result != tt.expected {
				t.Errorf("bitbucketCloudStateMapLegacy.Map(%s) = %s, expected %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCloudReporter_url(t *testing.T) {
	reporter := NewCloud(CloudConfig{
		BaseURL: bitbucketAPIBaseURL,
	})

	tests := []struct {
		name        string
		event       domain.Event
		expected    string
		expectError bool
	}{
		{
			name: "valid with workspace",
			event: domain.Event{
				CommitSHA: testCommitAbc123,
				Repo: domain.Repo{
					Workspace: testWorkspace,
					Name:      testRepo,
				},
			},
			expected: bitbucketAPIBaseURL + "/2.0/repositories/" + testWorkspace + "/" + testRepo + "/commit/" + testCommitAbc123 + "/statuses/build",
		},
		{
			name: "fallback to owner when workspace empty",
			event: domain.Event{
				CommitSHA: "def456",
				Repo: domain.Repo{
					Owner: "myowner",
					Name:  "testRepo",
				},
			},
			expected: bitbucketAPIBaseURL + "/2.0/repositories/myowner/" + testRepo + "/commit/def456/statuses/build",
		},
		{
			name: "use event APIBaseURL",
			event: domain.Event{
				APIBaseURL: "https://custom.bitbucket.com",
				CommitSHA:  "xyz789",
				Repo: domain.Repo{
					Workspace: testWorkspace,
					Name:      testRepo,
				},
			},
			expected: "https://custom.bitbucket.com/2.0/repositories/" + testWorkspace + "/" + testRepo + "/commit/xyz789/statuses/build",
		},
		{
			name: "trim trailing slash from base URL",
			event: domain.Event{
				APIBaseURL: bitbucketAPIBaseURL + "/",
				CommitSHA:  testCommitAbc123,
				Repo: domain.Repo{
					Workspace: testWorkspace,
					Name:      testRepo,
				},
			},
			expected: bitbucketAPIBaseURL + "/2.0/repositories/" + testWorkspace + "/" + testRepo + "/commit/" + testCommitAbc123 + "/statuses/build",
		},
		{
			name: "missing workspace and owner",
			event: domain.Event{
				CommitSHA: testCommitAbc123,
				Repo: domain.Repo{
					Name: "testRepo",
				},
			},
			expectError: true,
		},
		{
			name: "missing repo name",
			event: domain.Event{
				CommitSHA: testCommitAbc123,
				Repo: domain.Repo{
					Workspace: testWorkspace,
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := reporter.url(tt.event)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if url != tt.expected {
					t.Errorf("url = %s, expected %s", url, tt.expected)
				}
			}
		})
	}
}

func TestCloudReporter_payload(t *testing.T) {
	reporter := NewCloud(CloudConfig{})

	tests := []struct {
		name     string
		event    domain.Event
		validate func(t *testing.T, payload map[string]string)
	}{
		{
			name: "full event",
			event: domain.Event{
				State:       domain.StateSuccess,
				Context:     testBuildContext,
				Description: "Build succeeded",
				TargetURL:   testCIURL,
				RunName:     testRunID123,
			},
			validate: func(t *testing.T, payload map[string]string) {
				if payload["key"] != testBuildContext {
					t.Errorf("key = %s, expected testBuildContext", payload["key"])
				}
				if payload["state"] != stateSuccessful {
					t.Errorf("state = %s, expected stateSuccessful", payload["state"])
				}
				if payload["name"] != testBuildContext {
					t.Errorf("name = %s, expected testBuildContext", payload["name"])
				}
				if payload["description"] != "Build succeeded" {
					t.Errorf("description = %s, expected Build succeeded", payload["description"])
				}
				if payload["url"] != testCIURL {
					t.Errorf("url = %s, expected testCIURL", payload["url"])
				}
			},
		},
		{
			name: "fallback key when context empty",
			event: domain.Event{
				State:   domain.StateSuccess,
				RunName: "run-456",
			},
			validate: func(t *testing.T, payload map[string]string) {
				if payload["key"] != "tekton-run-456" {
					t.Errorf("key = %s, expected tekton-run-456", payload["key"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := reporter.payload(tt.event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			payload, ok := result.(map[string]string)
			if !ok {
				t.Fatal("payload is not map[string]string")
			}
			tt.validate(t, payload)
		})
	}
}

func TestCloudReporter_payload_ValidationErrors(t *testing.T) {
	reporter := NewCloud(CloudConfig{})

	tests := []struct {
		name        string
		event       domain.Event
		expectError string
	}{
		{
			name: "key exceeds 40 chars",
			event: domain.Event{
				State:   domain.StateSuccess,
				Context: "this-is-a-very-long-context-name-that-exceeds-forty-characters",
				RunName: "testRunID123",
			},
			expectError: `field "key" exceeds limit (40 chars, got 62)`,
		},
		{
			name: "name exceeds 255 chars",
			event: domain.Event{
				State:   domain.StateSuccess,
				Context: strings.Repeat("a", 260),
				RunName: "short",
			},
			expectError: `field "key" exceeds limit (40 chars, got 260)`,
		},
		{
			name: "description exceeds 255 chars",
			event: domain.Event{
				State:       domain.StateSuccess,
				Context:     "test",
				Description: strings.Repeat("a", 300),
			},
			expectError: `field "description" exceeds limit (255 chars, got 300)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reporter.payload(tt.event)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.expectError {
				t.Errorf("error = %q, expected %q", err.Error(), tt.expectError)
			}
		})
	}
}

func TestCloudReporter_auth(t *testing.T) {
	reporter := NewCloud(CloudConfig{
		Username:    "myuser",
		AppPassword: "mypass",
	})

	req, _ := http.NewRequest(http.MethodPost, "testExampleURL", nil)
	reporter.auth(req)

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		t.Errorf("expected Basic auth, got %s", auth)
	}

	// Decode and verify credentials
	encoded := strings.TrimPrefix(auth, "Basic ")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("failed to decode auth: %v", err)
	}

	expected := "myuser:mypass"
	if string(decoded) != expected {
		t.Errorf("decoded credentials = %s, expected %s", string(decoded), expected)
	}
}

// ============================================================
//   Bitbucket Server Tests
// ============================================================

func TestNewServer(t *testing.T) {
	cfg := ServerConfig{
		Token:   testToken,
		BaseURL: testBitbucketEnterpriseURL,
	}

	reporter := NewServer(cfg)

	if reporter == nil {
		t.Fatal("NewServer returned nil")
	}
	if reporter.cfg.Token != "testToken" {
		t.Errorf("expected token 'testToken', got %s", reporter.cfg.Token)
	}
	if reporter.cfg.BaseURL != "testBitbucketEnterpriseURL" {
		t.Errorf("expected base URL 'testBitbucketEnterpriseURL', got %s", reporter.cfg.BaseURL)
	}
	if reporter.base == nil {
		t.Fatal("base notifier not initialized")
	}
	if reporter.base.UserAgent != notifier.UserAgent {
		t.Errorf("expected user agent 'notifier.UserAgent', got %s", reporter.base.UserAgent)
	}
}

func TestServerReporter_Name(t *testing.T) {
	reporter := NewServer(ServerConfig{})
	if name := reporter.Name(); name != "bitbucket-server" {
		t.Errorf("expected name 'bitbucket-server', got %s", name)
	}
}

func TestServerReporter_Notify_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify HTTP method
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify auth header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer testToken" {
			t.Errorf("expected Bearer testToken, got %s", auth)
		}

		// Verify URL path
		expectedPath := "/rest/build-status/1.0/commits/testCommitAbc123"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}

		// Parse and verify payload
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}

		if payload["state"] != stateSuccessful {
			t.Errorf("expected state stateSuccessful, got %s", payload["state"])
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter := NewServer(ServerConfig{
		Token:   testToken,
		BaseURL: server.URL,
	})

	event := domain.Event{
		State:       domain.StateSuccess,
		Context:     testContext,
		Description: testDescription,
		TargetURL:   testExampleURL,
		CommitSHA:   testCommitAbc123,
	}

	err := reporter.Notify(context.Background(), event)
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}
}

func TestServerReporter_Notify_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer server.Close()

	reporter := NewServer(ServerConfig{
		Token:   "invalid-token",
		BaseURL: server.URL,
	})

	event := domain.Event{
		State:     domain.StateSuccess,
		CommitSHA: testCommitAbc123,
	}

	err := reporter.Notify(context.Background(), event)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 error, got %v", err)
	}
}

func TestMapServerState(t *testing.T) {
	tests := []struct {
		input    domain.State
		expected string
	}{
		{domain.StateSuccess, stateSuccessful},
		{domain.StateFailure, stateFailed},
		{domain.StateError, stateFailed},
		{domain.StateCanceled, stateFailed},
		{domain.StatePending, stateInProgress},
		{domain.StateRunning, stateInProgress},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := bitbucketServerStateMapLegacy.Map(tt.input, "INPROGRESS")
			if result != tt.expected {
				t.Errorf("bitbucketServerStateMapLegacy.Map(%s) = %s, expected %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestServerReporter_url(t *testing.T) {
	reporter := NewServer(ServerConfig{
		BaseURL: testBitbucketEnterpriseURL,
	})

	tests := []struct {
		name        string
		event       domain.Event
		expected    string
		expectError bool
	}{
		{
			name: "valid with base URL from config",
			event: domain.Event{
				CommitSHA: testCommitAbc123,
			},
			expected: testBitbucketEnterpriseURL + "/rest/build-status/1.0/commits/" + testCommitAbc123,
		},
		{
			name: "use event APIBaseURL",
			event: domain.Event{
				APIBaseURL: "https://custom.bitbucket.com",
				CommitSHA:  "def456",
			},
			expected: "https://custom.bitbucket.com/rest/build-status/1.0/commits/def456",
		},
		{
			name: "trim trailing slash",
			event: domain.Event{
				APIBaseURL: "testBitbucketEnterpriseURL/",
				CommitSHA:  "xyz789",
			},
			expected: testBitbucketEnterpriseURL + "/rest/build-status/1.0/commits/xyz789",
		},
		{
			name: "missing base URL",
			event: domain.Event{
				CommitSHA: testCommitAbc123,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Override base URL for missing base URL test
			if tt.expectError && tt.name == "missing base URL" {
				reporter = NewServer(ServerConfig{})
			}

			url, err := reporter.url(tt.event)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if url != tt.expected {
					t.Errorf("url = %s, expected %s", url, tt.expected)
				}
			}
		})
	}
}

func TestServerReporter_payload(t *testing.T) {
	reporter := NewServer(ServerConfig{})

	tests := []struct {
		name     string
		event    domain.Event
		validate func(t *testing.T, payload map[string]string)
	}{
		{
			name: "full event",
			event: domain.Event{
				State:       domain.StateFailure,
				Context:     testBuildContext,
				Description: "Build failed",
				TargetURL:   testCIURL,
				RunName:     testRunID123,
			},
			validate: func(t *testing.T, payload map[string]string) {
				if payload["state"] != stateFailed {
					t.Errorf("state = %s, expected stateFailed", payload["state"])
				}
				if payload["key"] != testBuildContext {
					t.Errorf("key = %s, expected testBuildContext", payload["key"])
				}
				if payload["name"] != testBuildContext {
					t.Errorf("name = %s, expected testBuildContext", payload["name"])
				}
				if payload["description"] != "Build failed" {
					t.Errorf("description = %s, expected Build failed", payload["description"])
				}
				if payload["url"] != testCIURL {
					t.Errorf("url = %s, expected testCIURL", payload["url"])
				}
			},
		},
		{
			name: "fallback key when context empty",
			event: domain.Event{
				State:   domain.StateRunning,
				RunName: "run-789",
			},
			validate: func(t *testing.T, payload map[string]string) {
				if payload["key"] != "tekton-run-789" {
					t.Errorf("key = %s, expected tekton-run-789", payload["key"])
				}
			},
		},
		{
			name: "includes parent when Repo.Project set",
			event: domain.Event{
				State:   domain.StateSuccess,
				Context: testContext,
				RunName: testRunID123,
				Repo: domain.Repo{
					Project: "MYPROJECT",
				},
			},
			validate: func(t *testing.T, payload map[string]string) {
				if payload["parent"] != "MYPROJECT" {
					t.Errorf("parent = %s, expected MYPROJECT", payload["parent"])
				}
			},
		},
		{
			name: "omits parent when Repo.Project empty",
			event: domain.Event{
				State:   domain.StateSuccess,
				Context: testContext,
				RunName: testRunID123,
			},
			validate: func(t *testing.T, payload map[string]string) {
				if _, exists := payload["parent"]; exists {
					t.Errorf("parent key should not exist when Repo.Project empty, got %s", payload["parent"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := reporter.payload(tt.event)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			payload, ok := result.(map[string]string)
			if !ok {
				t.Fatal("payload is not map[string]string")
			}
			tt.validate(t, payload)
		})
	}
}

func TestServerReporter_payload_ValidationErrors(t *testing.T) {
	reporter := NewServer(ServerConfig{})

	tests := []struct {
		name        string
		event       domain.Event
		expectError string
	}{
		{
			name: "key exceeds 40 chars",
			event: domain.Event{
				State:   domain.StateSuccess,
				Context: "this-is-a-very-long-context-name-that-exceeds-forty-characters",
				RunName: "testRunID123",
			},
			expectError: `field "key" exceeds limit (40 chars, got 62)`,
		},
		{
			name: "name exceeds 255 chars",
			event: domain.Event{
				State:   domain.StateSuccess,
				Context: strings.Repeat("a", 260),
				RunName: "short",
			},
			expectError: `field "key" exceeds limit (40 chars, got 260)`,
		},
		{
			name: "description exceeds 255 chars",
			event: domain.Event{
				State:       domain.StateSuccess,
				Context:     "test",
				Description: strings.Repeat("a", 300),
			},
			expectError: `field "description" exceeds limit (255 chars, got 300)`,
		},
		{
			name: "parent exceeds 255 chars",
			event: domain.Event{
				State:   domain.StateSuccess,
				Context: "test",
				Repo: domain.Repo{
					Project: strings.Repeat("a", 260),
				},
			},
			expectError: `field "parent" exceeds limit (255 chars, got 260)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reporter.payload(tt.event)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.expectError {
				t.Errorf("error = %q, expected %q", err.Error(), tt.expectError)
			}
		})
	}
}

func TestServerReporter_auth(t *testing.T) {
	reporter := NewServer(ServerConfig{
		Token: "my-secret-token",
	})

	req, _ := http.NewRequest(http.MethodPost, "testExampleURL", nil)
	reporter.auth(req)

	auth := req.Header.Get("Authorization")
	expected := "Bearer my-secret-token"
	if auth != expected {
		t.Errorf("auth header = %s, expected %s", auth, expected)
	}
}

// ============================================================
//   Helper Function Tests
// ============================================================
// (truncate tests moved to internal/stringutil/stringutil_test.go)
