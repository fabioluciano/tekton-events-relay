package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testInstanceName       = "main"
	testToken              = "token123"
	testTokenShort         = "token"
	testBaseURLGitHub      = "https://github.com"
	testBaseURLGitLab      = "https://gitlab.com"
	testBaseURLBitbucket   = "https://bitbucket.org"
	testActionNameStatus   = "status"
	testVariantCloud       = BitbucketVariantCloud
	testVariantServer      = BitbucketVariantServer
	testInstanceNameBB     = "bb"
	testInstanceNameAlerts = "alerts"
	testUsername           = "user"
	testPassword           = "pass"
	testTeamName           = "team"
	testActionNamePR       = "pr"
	errMissingAuth         = "enabled instance missing required field 'auth'"
)

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		SCM: SCMConfig{
			GitHub: []GitHubInstance{
				{
					Name:    testInstanceName,
					Enabled: true,
					Auth:    &GitHubAuth{SecretFile: "/tmp/token"},
					BaseURL: testBaseURLGitHub,
				},
			},
		},
		Notifiers: NotifiersConfig{
			Slack: []SlackInstance{
				{
					Name:    "team-slack",
					Enabled: true,
					Auth:    &SlackAuth{WebhookURLFile: "https://hooks.slack.com/webhook"},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Expected valid config to pass validation, got: %v", err)
	}
}

func TestValidate_InvalidCEL_GitHub(t *testing.T) {
	cfg := &Config{
		SCM: SCMConfig{
			GitHub: []GitHubInstance{
				{
					Name:    testInstanceName,
					Enabled: true,
					Auth:    &GitHubAuth{SecretFile: "/tmp/token"},
					BaseURL: testBaseURLGitHub,
					Actions: []Action{
						{
							Name:    testActionNameStatus,
							Type:    ActionTypeCommitStatus,
							Enabled: true,
							When:    "event.State == ", // Invalid CEL
						},
					},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation to fail for invalid CEL expression")
	}
	if !strings.Contains(err.Error(), "scm.github[0].actions[0].when") {
		t.Errorf("Expected error path 'scm.github[0].actions[0].when', got: %v", err)
	}
}

func TestValidate_InvalidCEL_AllActions(t *testing.T) {
	tests := []struct {
		name        string
		action      Action
		expectedErr string
	}{
		{
			name: "pr_comment invalid",
			action: Action{
				Name: "pr",
				Type: ActionTypePRComment,
				When: "invalid CEL &&",
			},
			expectedErr: "actions[0].when",
		},
		{
			name: "issue_comment invalid",
			action: Action{
				Name: "issue",
				Type: ActionTypeIssueComment,
				When: "event.State ==",
			},
			expectedErr: "actions[0].when",
		},
		{
			name: "label invalid",
			action: Action{
				Name: "label",
				Type: ActionTypeLabel,
				When: "missing()",
			},
			expectedErr: "actions[0].when",
		},
		{
			name: "discussion_comment invalid",
			action: Action{
				Name: "disc",
				Type: ActionTypeDiscussionComment,
				When: "event.State ===",
			},
			expectedErr: "actions[0].when",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				SCM: SCMConfig{
					GitHub: []GitHubInstance{
						{
							Name:    "main", //nolint:goconst
							Enabled: true,
							Auth:    &GitHubAuth{SecretFile: "token"}, //nolint:goconst
							BaseURL: "https://github.com",             //nolint:goconst
							Actions: []Action{tt.action},
						},
					},
				},
			}

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Expected validation to fail")
			}
			if !strings.Contains(err.Error(), tt.expectedErr) {
				t.Errorf("Expected error to contain '%s', got: %v", tt.expectedErr, err)
			}
		})
	}
}

func TestValidate_BitbucketVariant(t *testing.T) {
	tests := []struct {
		name        string
		variant     string
		username    string
		appPassword string
		token       string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "invalid variant",
			variant:     "foo",
			expectError: true,
			errorMsg:    "variant must be 'cloud' or 'server', got 'foo'",
		},
		{
			name:        "cloud without username",
			variant:     "cloud", //nolint:goconst
			appPassword: "pass",
			expectError: true,
			errorMsg:    "variant 'cloud' requires",
		},
		{
			name:        "cloud without app_password",
			variant:     "cloud",
			username:    "user",
			expectError: true,
			errorMsg:    "variant 'cloud' requires",
		},
		{
			name:        "cloud valid",
			variant:     "cloud",
			username:    "user",
			appPassword: "pass",
			expectError: false,
		},
		{
			name:        "server without token",
			variant:     "server",
			expectError: true,
			errorMsg:    "variant 'server' requires",
		},
		{
			name:        "server valid",
			variant:     "server",
			token:       "token123", //nolint:goconst
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				SCM: SCMConfig{
					Bitbucket: []BitbucketInstance{
						{
							Name:    "bb",
							Enabled: true,
							Variant: tt.variant,
							Auth: func() *BitbucketAuth {
								auth := &BitbucketAuth{}
								if tt.variant == BitbucketVariantCloud {
									auth.UsernameFile = tt.username
									auth.AppPasswordFile = tt.appPassword
								}
								if tt.variant == BitbucketVariantServer {
									auth.TokenFile = tt.token
								}
								if auth.UsernameFile == "" && auth.AppPasswordFile == "" && auth.TokenFile == "" {
									return nil
								}
								return auth
							}(),
							BaseURL: "https://bitbucket.org",
						},
					},
				},
			}

			err := cfg.Validate()
			if tt.expectError {
				if err == nil {
					t.Fatal("Expected validation to fail")
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain '%s', got: %v", tt.errorMsg, err)
				}
			} else if err != nil {
				t.Errorf("Expected validation to pass, got: %v", err)
			}
		})
	}
}

func TestValidate_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		expectedErr string
	}{
		{
			name: "github missing token",
			cfg: &Config{
				SCM: SCMConfig{
					GitHub: []GitHubInstance{
						{
							Name:    "main",
							Enabled: true,
							BaseURL: "https://github.com",
						},
					},
				},
			},
			expectedErr: "either 'auth.secret_file' or GitHub App credentials",
		},
		{
			name: "github missing base_url",
			cfg: &Config{
				SCM: SCMConfig{
					GitHub: []GitHubInstance{
						{
							Name:    "main",
							Enabled: true,
							Auth:    &GitHubAuth{SecretFile: "token123"},
						},
					},
				},
			},
			expectedErr: "scm.github[0].base_url",
		},
		{
			name: "gitlab missing token",
			cfg: &Config{
				SCM: SCMConfig{
					GitLab: []GitLabInstance{
						{
							Name:    "main",
							Variant: "saas",
							Enabled: true,
							BaseURL: testBaseURLGitLab,
						},
					},
				},
			},
			expectedErr: errMissingAuth,
		},
		{
			name: "gitlab missing variant",
			cfg: &Config{
				SCM: SCMConfig{
					GitLab: []GitLabInstance{
						{
							Name:    "main",
							Enabled: true,
							Auth:    &GitLabAuth{SecretFile: "token"},
							BaseURL: testBaseURLGitLab,
						},
					},
				},
			},
			expectedErr: "variant must be 'saas' or 'self-managed'",
		},
		{
			name: "gitlab invalid variant",
			cfg: &Config{
				SCM: SCMConfig{
					GitLab: []GitLabInstance{
						{
							Name:    "main",
							Variant: "cloud",
							Enabled: true,
							Auth:    &GitLabAuth{SecretFile: "token"},
							BaseURL: testBaseURLGitLab,
						},
					},
				},
			},
			expectedErr: "variant must be 'saas' or 'self-managed', got 'cloud'",
		},
		{
			name: "slack missing auth",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					Slack: []SlackInstance{
						{
							Name:    "team",
							Enabled: true,
						},
					},
				},
			},
			expectedErr: errMissingAuth,
		},
		{
			name: "teams missing auth",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					Teams: []TeamsInstance{
						{
							Name:    "team",
							Enabled: true,
						},
					},
				},
			},
			expectedErr: errMissingAuth,
		},
		{
			name: "discord missing auth",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					Discord: []DiscordInstance{
						{
							Name:    "team",
							Enabled: true,
						},
					},
				},
			},
			expectedErr: errMissingAuth,
		},
		{
			name: "pagerduty missing auth",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					PagerDuty: []PagerDutyInstance{
						{
							Name:    "oncall",
							Enabled: true,
						},
					},
				},
			},
			expectedErr: errMissingAuth,
		},
		{
			name: "datadog missing auth",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					Datadog: []DatadogInstance{
						{
							Name:    "monitoring",
							Enabled: true,
						},
					},
				},
			},
			expectedErr: errMissingAuth,
		},
		{
			name: "webhook missing url",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					Webhook: []WebhookInstance{
						{
							Name:    "custom",
							Enabled: true,
						},
					},
				},
			},
			expectedErr: "enabled instance missing required field 'url_file'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatal("Expected validation to fail")
			}
			if !strings.Contains(err.Error(), tt.expectedErr) {
				t.Errorf("Expected error '%s', got: %v", tt.expectedErr, err)
			}
		})
	}
}

func TestValidate_DuplicateInstanceNames(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		expectedErr string
	}{
		{
			name: "duplicate github names",
			cfg: &Config{
				SCM: SCMConfig{
					GitHub: []GitHubInstance{
						{
							Name:    "main-instance",
							Enabled: true,
							Auth:    &GitHubAuth{SecretFile: "token1"},
							BaseURL: "https://github.com",
						},
						{
							Name:    "main-instance",
							Enabled: true,
							Auth:    &GitHubAuth{SecretFile: "token2"},
							BaseURL: "https://ghe.company.com",
						},
					},
				},
			},
			expectedErr: "scm.github: duplicate instance name 'main-instance'",
		},
		{
			name: "duplicate gitlab names",
			cfg: &Config{
				SCM: SCMConfig{
					GitLab: []GitLabInstance{
						{
							Name:    "prod",
							Variant: "saas",
							Enabled: true,
							Auth:    &GitLabAuth{SecretFile: "token1"},
							BaseURL: testBaseURLGitLab,
						},
						{
							Name:    "prod",
							Variant: "self-managed",
							Enabled: true,
							Auth:    &GitLabAuth{SecretFile: "token2"},
							BaseURL: "https://gitlab.company.com",
						},
					},
				},
			},
			expectedErr: "scm.gitlab: duplicate instance name 'prod'",
		},
		{
			name: "duplicate bitbucket names",
			cfg: &Config{
				SCM: SCMConfig{
					Bitbucket: []BitbucketInstance{
						{
							Name:    "bb",
							Enabled: true,
							Variant: "cloud",
							Auth:    &BitbucketAuth{UsernameFile: "user1", AppPasswordFile: "pass1"},
							BaseURL: "https://bitbucket.org",
						},
						{
							Name:    "bb",
							Enabled: true,
							Variant: "server",
							Auth:    &BitbucketAuth{TokenFile: "token"},
							BaseURL: "https://bb.company.com",
						},
					},
				},
			},
			expectedErr: "scm.bitbucket: duplicate instance name 'bb'",
		},
		{
			name: "duplicate slack names",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					Slack: []SlackInstance{
						{
							Name:    "alerts", //nolint:goconst
							Enabled: true,
							Auth:    &SlackAuth{WebhookURLFile: "https://hooks.slack.com/1"},
						},
						{
							Name:    "alerts",
							Enabled: true,
							Auth:    &SlackAuth{WebhookURLFile: "https://hooks.slack.com/2"},
						},
					},
				},
			},
			expectedErr: "notifiers.slack: duplicate instance name 'alerts'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatal("Expected validation to fail")
			}
			if !strings.Contains(err.Error(), tt.expectedErr) {
				t.Errorf("Expected error '%s', got: %v", tt.expectedErr, err)
			}
		})
	}
}

func TestValidate_NotifierCEL(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		expectedErr string
	}{
		{
			name: "slack invalid CEL",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					Slack: []SlackInstance{
						{
							Name:    "alerts",
							Enabled: true,
							Auth:    &SlackAuth{WebhookURLFile: "https://hooks.slack.com/webhook"},
							When:    "event.State ==",
						},
					},
				},
			},
			expectedErr: "notifiers.slack[0].when",
		},
		{
			name: "teams invalid CEL",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					Teams: []TeamsInstance{
						{
							Name:    "alerts",
							Enabled: true,
							Auth:    &TeamsAuth{WebhookURLFile: "https://outlook.office.com/webhook"},
							When:    "invalid &&",
						},
					},
				},
			},
			expectedErr: "notifiers.teams[0].when",
		},
		{
			name: "discord invalid CEL",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					Discord: []DiscordInstance{
						{
							Name:    "alerts",
							Enabled: true,
							Auth:    &DiscordAuth{WebhookURLFile: "https://discord.com/api/webhooks/123"},
							When:    "event.State ===",
						},
					},
				},
			},
			expectedErr: "notifiers.discord[0].when",
		},
		{
			name: "pagerduty invalid CEL",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					PagerDuty: []PagerDutyInstance{
						{
							Name:    "oncall",
							Enabled: true,
							Auth:    &PagerDutyAuth{IntegrationKeyFile: "key123"},
							When:    "missing()",
						},
					},
				},
			},
			expectedErr: "notifiers.pagerduty[0].when",
		},
		{
			name: "datadog invalid CEL",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					Datadog: []DatadogInstance{
						{
							Name:    "monitoring",
							Enabled: true,
							Auth:    &DatadogAuth{APIKeyFile: "key123"},
							When:    "event.State = ",
						},
					},
				},
			},
			expectedErr: "notifiers.datadog[0].when",
		},
		{
			name: "webhook invalid CEL",
			cfg: &Config{
				Notifiers: NotifiersConfig{
					Webhook: []WebhookInstance{
						{
							Name:    "custom",
							Enabled: true,
							URLFile: "https://example.com/webhook",
							When:    "event.State ==",
						},
					},
				},
			},
			expectedErr: "notifiers.webhook[0].when",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatal("Expected validation to fail")
			}
			if !strings.Contains(err.Error(), tt.expectedErr) {
				t.Errorf("Expected error to contain '%s', got: %v", tt.expectedErr, err)
			}
		})
	}
}

func TestValidate_DisabledInstancesSkipValidation(t *testing.T) {
	cfg := &Config{
		SCM: SCMConfig{
			GitHub: []GitHubInstance{
				{
					Name:    "disabled",
					Enabled: false,
					// Missing token and base_url - should not fail
				},
			},
		},
		Notifiers: NotifiersConfig{
			Slack: []SlackInstance{
				{
					Name:    "disabled",
					Enabled: false,
					// Missing webhook_url - should not fail
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Expected disabled instances to skip validation, got: %v", err)
	}
}

func TestValidate_AllSCMProviders(t *testing.T) {
	cfg := &Config{
		SCM: SCMConfig{
			GitHub: []GitHubInstance{
				{
					Name:    "gh",
					Enabled: true,
					Auth:    &GitHubAuth{SecretFile: "token"},
					BaseURL: "https://github.com",
					Actions: []Action{
						{Name: "status", Type: ActionTypeCommitStatus, When: "event.State == 'succeeded'"}, //nolint:goconst
					},
				},
			},
			GitLab: []GitLabInstance{
				{
					Name:    "gl",
					Variant: "saas",
					Enabled: true,
					Auth:    &GitLabAuth{SecretFile: "token"},
					BaseURL: "https://gitlab.com",
					Actions: []Action{
						{Name: "status", Type: ActionTypeCommitStatus, When: "event.State == 'failed'"},
					},
				},
			},
			Gitea: []GiteaInstance{
				{
					Name:    "gt",
					Enabled: true,
					Auth:    &GiteaAuth{SecretFile: "token"},
					BaseURL: "https://gitea.com",
					Actions: []Action{
						{Name: "pr", Type: ActionTypePRComment, When: "event.State == 'running'"},
					},
				},
			},
			Azure: []AzureInstance{
				{
					Name:       "az",
					Enabled:    true,
					SecretFile: "token",
					BaseURL:    "https://dev.azure.com",
					Actions: []Action{
						{Name: "label", Type: ActionTypeLabel, When: "event.State == 'succeeded'"},
					},
				},
			},
			Bitbucket: []BitbucketInstance{
				{
					Name:    "bb",
					Enabled: true,
					Variant: "cloud",
					Auth:    &BitbucketAuth{UsernameFile: "user", AppPasswordFile: "pass"},
					BaseURL: "https://bitbucket.org",
					Actions: []Action{
						{Name: "pr", Type: ActionTypePRComment, When: "event.PRNumber > 0"},
					},
				},
			},
			SourceHut: []SourceHutInstance{
				{
					Name:    "sr",
					Enabled: true,
					Auth:    &SourceHutAuth{SecretFile: "token"},
					BaseURL: "https://sr.ht",
					Actions: []Action{
						{Name: "status", Type: ActionTypeCommitStatus, When: "event.State != 'pending'"},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Expected all SCM providers with valid CEL to pass, got: %v", err)
	}
}

func TestLoad_CallsValidate(t *testing.T) {
	// Create a temp config file with invalid Bitbucket variant
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `
scm:
  bitbucket:
    - name: test
      enabled: true
      variant: invalid_variant
      base_url: https://bitbucket.org
`
	if err := os.WriteFile(configPath, []byte(yaml), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Expected Load to fail validation")
	}
	if !strings.Contains(err.Error(), "config validation") {
		t.Errorf("Expected 'config validation' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "variant must be 'cloud' or 'server'") {
		t.Errorf("Expected variant error, got: %v", err)
	}
}

func TestConfig_SecurityDefaults(t *testing.T) {
	cfg := &Config{
		Server: Server{
			Addr: DefaultServerAddr,
		},
	}

	// MaxBodySize zero means "use default" at runtime
	if cfg.Server.MaxBodySize != 0 {
		t.Errorf("expected zero MaxBodySize (runtime default), got %d", cfg.Server.MaxBodySize)
	}

	if cfg.Server.RateLimit.Enabled {
		t.Error("rate limit should be disabled by default")
	}

	if cfg.Server.Auth.Enabled {
		t.Error("auth should be disabled by default")
	}
}

func TestConfig_AuthTypes(t *testing.T) {
	tests := []struct {
		name     string
		authType string
		wantErr  bool
	}{
		{"hmac-sha256", "hmac-sha256", false},
		{"bearer", "bearer", false},
		{"invalid basic", "basic", true},
		{"invalid empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Server: Server{
					Auth: AuthConfig{ //nolint:gosec // test data
						Enabled:    true,
						Type:       tt.authType,
						SecretFile: "${WEBHOOK_SECRET}",
					},
				},
			}
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfig_AuthMissingSecretFile(t *testing.T) {
	cfg := &Config{
		Server: Server{
			Auth: AuthConfig{
				Enabled: true,
				Type:    "bearer",
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing secret_file")
	}
	if !strings.Contains(err.Error(), "missing 'secret_file'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ValidCELExpressions(t *testing.T) {
	tests := []struct {
		name string
		when string
	}{
		{"simple equality", "event.State == 'succeeded'"},
		{"not equal", "event.State != 'pending'"},
		{"and condition", "event.State == 'failed' && event.PRNumber > 0"},
		{"or condition", "event.State == 'succeeded' || event.State == 'failed'"},
		{"string contains", "event.RunName.contains('prod')"},
		{"nested map access", "event.Repo.Owner == 'myorg'"},
		{"greater than", "event.PRNumber > 100"},
		{"complex", "event.State == 'succeeded' && event.Repo.Owner == 'tektoncd' && event.PRNumber > 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				SCM: SCMConfig{
					GitHub: []GitHubInstance{
						{
							Name:    "test",
							Enabled: true,
							Auth:    &GitHubAuth{SecretFile: "token"},
							BaseURL: "https://github.com",
							Actions: []Action{
								{Name: "status", Type: ActionTypeCommitStatus, When: tt.when},
							},
						},
					},
				},
			}

			if err := cfg.Validate(); err != nil {
				t.Errorf("Valid CEL '%s' failed validation: %v", tt.when, err)
			}
		})
	}
}
