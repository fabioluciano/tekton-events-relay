package gitlab

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	// Test constants
	testNameGitLab          = "gitlab"
	testToken               = "token"
	testTokenTest           = "test-token"
	testGitLabExampleAPIURL = "https://gitlab.example.com/api/v4"
	testGitLabCustomAPIURL  = "https://custom.gitlab.com/api/v4"
	testOrgName             = "myorg"
	testRepoName            = "myrepo"
	testCommitSHA           = "abc123"
	testContextCITest       = "ci/test"
	testContextCIBuild      = "ci/build"
	testDescriptionPassed   = "Tests passed"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "basic config",
			cfg:  Config{Name: testNameGitLab, Token: testTokenTest},
		},
		{
			name: "config with base url",
			cfg:  Config{Name: testNameGitLab, Token: testTokenTest, BaseURL: testGitLabExampleAPIURL},
		},
		{
			name: "empty token",
			cfg:  Config{Name: testNameGitLab, Token: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(tt.cfg)
			if r == nil {
				t.Fatal("expected reporter, got nil")
			}
			if r.base == nil {
				t.Fatal("expected base to be initialized")
			}
			if r.cfg.Name != tt.cfg.Name {
				t.Errorf("cfg.Name = %q, want %q", r.cfg.Name, tt.cfg.Name)
			}
			if r.cfg.Token != tt.cfg.Token {
				t.Errorf("cfg.Token = %q, want %q", r.cfg.Token, tt.cfg.Token)
			}
		})
	}
}

func TestNewCloud(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		wantName    string
		wantBaseURL string
	}{
		{
			name:        "cloud with empty base url",
			cfg:         Config{Token: testTokenTest},
			wantName:    nameGitLabCloud,
			wantBaseURL: gitLabAPIBaseURL,
		},
		{
			name:        "cloud with custom base url",
			cfg:         Config{Token: testTokenTest, BaseURL: testGitLabCustomAPIURL},
			wantName:    nameGitLabCloud,
			wantBaseURL: testGitLabCustomAPIURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewCloud(tt.cfg)
			if r == nil {
				t.Fatal("expected reporter, got nil")
			}
			if r.cfg.Name != tt.wantName {
				t.Errorf("cfg.Name = %q, want %q", r.cfg.Name, tt.wantName)
			}
			if r.cfg.BaseURL != tt.wantBaseURL {
				t.Errorf("cfg.BaseURL = %q, want %q", r.cfg.BaseURL, tt.wantBaseURL)
			}
		})
	}
}

func TestNewServer(t *testing.T) {
	cfg := Config{Token: testTokenTest, BaseURL: testGitLabExampleAPIURL}
	r := NewServer(cfg)
	if r == nil {
		t.Fatal("expected reporter, got nil")
	}
	if r.cfg.Name != nameGitLabServer {
		t.Errorf("cfg.Name = %q, want gitlab-server", r.cfg.Name)
	}
	if r.cfg.BaseURL != cfg.BaseURL {
		t.Errorf("cfg.BaseURL = %q, want %q", r.cfg.BaseURL, cfg.BaseURL)
	}
}

func TestName(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		wantName string
	}{
		{
			name:     "explicit name",
			cfg:      Config{Name: "gitlab-test", Token: testToken},
			wantName: "gitlab-test",
		},
		{
			name:     "cloud name",
			cfg:      Config{Name: nameGitLabCloud, Token: testToken},
			wantName: nameGitLabCloud,
		},
		{
			name:     "server name",
			cfg:      Config{Name: nameGitLabServer, Token: testToken},
			wantName: nameGitLabServer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(tt.cfg)
			if got := r.Name(); got != tt.wantName {
				t.Errorf("Name() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestMapState(t *testing.T) {
	tests := []struct {
		state domain.State
		want  string
	}{
		{domain.StatePending, "pending"},
		{domain.StateRunning, "running"},
		{domain.StateSuccess, "success"},
		{domain.StateFailure, "failed"},
		{domain.StateError, "failed"},
		{domain.StateCanceled, "canceled"},
		{domain.State("unknown"), "pending"},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := gitlabStateMapLegacy.Map(tt.state, "pending")
			if got != tt.want {
				t.Errorf("gitlabStateMapLegacy.Map(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestProjectIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		event   domain.Event
		want    string
		wantErr bool
	}{
		{
			name: "with numeric ID",
			event: domain.Event{
				Repo: domain.Repo{ID: "12345"},
			},
			want:    "12345",
			wantErr: false,
		},
		{
			name: "with owner and name",
			event: domain.Event{
				Repo: domain.Repo{Owner: testOrgName, Name: testRepoName},
			},
			want:    "myorg%2Fmyrepo",
			wantErr: false,
		},
		{
			name: "with special characters in owner and name",
			event: domain.Event{
				Repo: domain.Repo{Owner: "my org", Name: "my/repo"},
			},
			want:    "my%20org%2Fmy%2Frepo",
			wantErr: false,
		},
		{
			name: "ID takes precedence over owner/name",
			event: domain.Event{
				Repo: domain.Repo{ID: "999", Owner: testOrgName, Name: testRepoName},
			},
			want:    "999",
			wantErr: false,
		},
		{
			name: "missing all identifiers",
			event: domain.Event{
				Repo: domain.Repo{},
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "only owner no name",
			event: domain.Event{
				Repo: domain.Repo{Owner: testOrgName},
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "only name no owner",
			event: domain.Event{
				Repo: domain.Repo{Name: testRepoName},
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := projectIdentifier(tt.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("projectIdentifier() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("projectIdentifier() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReporterURL(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		event   domain.Event
		want    string
		wantErr bool
	}{
		{
			name: "with numeric ID and config base URL",
			cfg:  Config{BaseURL: gitLabAPIBaseURL},
			event: domain.Event{
				Repo:      domain.Repo{ID: "12345"},
				CommitSHA: testCommitSHA,
			},
			want:    "https://gitlab.com/api/v4/projects/12345/statuses/abc123",
			wantErr: false,
		},
		{
			name: "with owner/name and config base URL",
			cfg:  Config{BaseURL: testGitLabExampleAPIURL},
			event: domain.Event{
				Repo:      domain.Repo{Owner: testOrgName, Name: testRepoName},
				CommitSHA: "def456",
			},
			want:    "https://gitlab.example.com/api/v4/projects/myorg%2Fmyrepo/statuses/def456",
			wantErr: false,
		},
		{
			name: "event API base URL overrides config",
			cfg:  Config{BaseURL: gitLabAPIBaseURL},
			event: domain.Event{
				APIBaseURL: testGitLabCustomAPIURL,
				Repo:       domain.Repo{ID: "999"},
				CommitSHA:  "xyz789",
			},
			want:    "https://custom.gitlab.com/api/v4/projects/999/statuses/xyz789",
			wantErr: false,
		},
		{
			name: "base URL with trailing slash",
			cfg:  Config{BaseURL: "https://gitlab.com/api/v4/"},
			event: domain.Event{
				Repo:      domain.Repo{ID: "12345"},
				CommitSHA: testCommitSHA,
			},
			want:    "https://gitlab.com/api/v4/projects/12345/statuses/abc123",
			wantErr: false,
		},
		{
			name: "missing base URL",
			cfg:  Config{},
			event: domain.Event{
				Repo:      domain.Repo{ID: "12345"},
				CommitSHA: testCommitSHA,
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "missing repo identifier",
			cfg:  Config{BaseURL: gitLabAPIBaseURL},
			event: domain.Event{
				Repo:      domain.Repo{},
				CommitSHA: testCommitSHA,
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(tt.cfg)
			got, err := r.url(tt.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("url() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("url() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReporterPayload(t *testing.T) {
	tests := []struct {
		name  string
		cfg   Config
		event domain.Event
		want  map[string]string
	}{
		{
			name: "complete payload with target URL",
			cfg:  Config{},
			event: domain.Event{
				State:       domain.StateSuccess,
				Context:     testContextCITest,
				Description: "All tests passed",
				TargetURL:   "https://ci.example.com/build/123",
			},
			want: map[string]string{
				fieldState:       stateSuccess,
				fieldName:        testContextCITest,
				fieldDescription: "All tests passed",
				fieldTargetURL:   "https://ci.example.com/build/123",
			},
		},
		{
			name: "payload without target URL",
			cfg:  Config{},
			event: domain.Event{
				State:       domain.StateRunning,
				Context:     testContextCIBuild,
				Description: "Building...",
			},
			want: map[string]string{
				fieldState:       stateRunning,
				fieldName:        testContextCIBuild,
				fieldDescription: "Building...",
			},
		},
		{
			name: "failed state",
			cfg:  Config{},
			event: domain.Event{
				State:       domain.StateFailure,
				Context:     "ci/lint",
				Description: "Linting failed",
			},
			want: map[string]string{
				fieldState:       stateFailed,
				fieldName:        "ci/lint",
				fieldDescription: "Linting failed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(tt.cfg)
			got, err := r.payload(tt.event)
			if err != nil {
				t.Fatalf("payload() unexpected error: %v", err)
			}
			gotMap, ok := got.(map[string]string)
			if !ok {
				t.Fatalf("payload() returned wrong type: %T", got)
			}
			// Check that all expected keys exist with correct values
			for k, wantV := range tt.want {
				gotV, exists := gotMap[k]
				if !exists {
					t.Errorf("payload() missing key %q", k)
					continue
				}
				if gotV != wantV {
					t.Errorf("payload()[%q] = %q, want %q", k, gotV, wantV)
				}
			}
			// Check that no unexpected keys exist
			for k := range gotMap {
				if _, expected := tt.want[k]; !expected {
					t.Errorf("payload() has unexpected key %q = %q", k, gotMap[k])
				}
			}
		})
	}
}

func TestReporterAuth(t *testing.T) {
	tests := []struct {
		name      string
		token     string
		wantToken string
	}{
		{
			name:      "with token",
			token:     "glpat-abc123",
			wantToken: "glpat-abc123",
		},
		{
			name:      "empty token",
			token:     "",
			wantToken: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(Config{Token: tt.token})
			req, err := http.NewRequest(http.MethodPost, "https://example.com", nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			r.auth(req)
			gotToken := req.Header.Get("PRIVATE-TOKEN")
			if gotToken != tt.wantToken {
				t.Errorf("auth() set PRIVATE-TOKEN = %q, want %q", gotToken, tt.wantToken)
			}
		})
	}
}

func TestNotify(t *testing.T) {
	tests := []struct {
		name           string
		cfg            Config
		event          domain.Event
		serverResponse int
		serverBody     string
		wantErr        bool
	}{
		{
			name: "successful notification",
			cfg:  Config{Token: testTokenTest},
			event: domain.Event{
				State:       domain.StateSuccess,
				Context:     testContextCITest,
				Description: testDescriptionPassed,
				CommitSHA:   testCommitSHA,
				Repo:        domain.Repo{ID: "12345"},
			},
			serverResponse: http.StatusOK,
			serverBody:     `{"id": 1}`,
			wantErr:        false,
		},
		{
			name: "successful notification with 201",
			cfg:  Config{Token: testTokenTest},
			event: domain.Event{
				State:       domain.StateRunning,
				Context:     testContextCIBuild,
				Description: "Building",
				CommitSHA:   "def456",
				Repo:        domain.Repo{Owner: testOrgName, Name: testRepoName},
			},
			serverResponse: http.StatusCreated,
			serverBody:     `{"id": 2}`,
			wantErr:        false,
		},
		{
			name: "server error 500",
			cfg:  Config{Token: testTokenTest},
			event: domain.Event{
				State:       domain.StateFailure,
				Context:     testContextCITest,
				Description: "Tests failed",
				CommitSHA:   "xyz789",
				Repo:        domain.Repo{ID: "99999"},
			},
			serverResponse: http.StatusInternalServerError,
			serverBody:     `{"error": "internal server error"}`,
			wantErr:        true,
		},
		{
			name: "unauthorized 401",
			cfg:  Config{Token: "invalid-token"},
			event: domain.Event{
				State:       domain.StateSuccess,
				Context:     testContextCITest,
				Description: testDescriptionPassed,
				CommitSHA:   testCommitSHA,
				Repo:        domain.Repo{ID: "12345"},
			},
			serverResponse: http.StatusUnauthorized,
			serverBody:     `{"message": "401 Unauthorized"}`,
			wantErr:        true,
		},
		{
			name: "not found 404",
			cfg:  Config{Token: testTokenTest},
			event: domain.Event{
				State:       domain.StateSuccess,
				Context:     testContextCITest,
				Description: testDescriptionPassed,
				CommitSHA:   "nonexistent",
				Repo:        domain.Repo{ID: "99999"},
			},
			serverResponse: http.StatusNotFound,
			serverBody:     `{"message": "404 Project Not Found"}`,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method
				if r.Method != http.MethodPost {
					t.Errorf("expected POST request, got %s", r.Method)
				}

				// Verify headers
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", ct)
				}
				if accept := r.Header.Get("Accept"); accept != "application/json" {
					t.Errorf("Accept = %q, want application/json", accept)
				}
				if ua := r.Header.Get("User-Agent"); ua != "tekton-events-relay" {
					t.Errorf("User-Agent = %q, want tekton-events-relay", ua)
				}
				if token := r.Header.Get("PRIVATE-TOKEN"); token != tt.cfg.Token {
					t.Errorf("PRIVATE-TOKEN = %q, want %q", token, tt.cfg.Token)
				}

				// Verify request body
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("failed to read request body: %v", err)
				}
				var payload map[string]string
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to unmarshal request body: %v", err)
				}

				// Verify payload contains expected fields
				expectedState := gitlabStateMapLegacy.Map(tt.event.State, "pending")
				if payload[fieldState] != expectedState {
					t.Errorf("payload state = %q, want %q", payload[fieldState], expectedState)
				}
				if payload[fieldName] != tt.event.Context {
					t.Errorf("payload name = %q, want %q", payload[fieldName], tt.event.Context)
				}
				if payload[fieldDescription] != tt.event.Description {
					t.Errorf("payload description = %q, want %q", payload[fieldDescription], tt.event.Description)
				}

				// Send response
				w.WriteHeader(tt.serverResponse)
				_, _ = w.Write([]byte(tt.serverBody))
			}))
			defer server.Close()

			// Configure reporter to use test server
			tt.cfg.BaseURL = server.URL
			r := New(tt.cfg)

			// Set API base URL in event
			tt.event.APIBaseURL = server.URL

			// Execute notification
			err := r.Notify(context.Background(), tt.event)

			// Check error expectation
			if (err != nil) != tt.wantErr {
				t.Errorf("Notify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNotifyWithInvalidEvent(t *testing.T) {
	tests := []struct {
		name  string
		event domain.Event
	}{
		{
			name: "missing repo identifier",
			event: domain.Event{
				State:       domain.StateSuccess,
				Context:     testContextCITest,
				Description: testDescriptionPassed,
				CommitSHA:   testCommitSHA,
				Repo:        domain.Repo{}, // empty repo
			},
		},
		{
			name: "missing API base URL and config base URL",
			event: domain.Event{
				State:       domain.StateSuccess,
				Context:     testContextCITest,
				Description: testDescriptionPassed,
				CommitSHA:   testCommitSHA,
				Repo:        domain.Repo{ID: "12345"},
				// no APIBaseURL set
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(Config{Token: testTokenTest}) // no BaseURL in config
			err := r.Notify(context.Background(), tt.event)
			if err == nil {
				t.Error("Notify() expected error, got nil")
			}
		})
	}
}
