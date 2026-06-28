package config

import (
	"strings"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

func TestMain(m *testing.M) {
	CELCompileFunc = func(expr string) error {
		_, err := cel.Compile(expr)
		return err
	}
	m.Run()
}

const invalidCELExpr = "invalid +++"

func TestValidateAll_ValidConfig(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		SCM: SCMConfig{
			GitHub: []GitHubInstance{
				{
					Name:    "main", //nolint:goconst
					Enabled: true,
					Auth:    &GitHubAuth{SecretFile: "token123"}, //nolint:goconst
					BaseURL: "https://github.com",                //nolint:goconst
					Actions: []Action{
						{
							Name: "status", //nolint:goconst
							Type: notifier.ActionCommitStatus,
							When: `event.State == "succeeded"`,
						},
					},
				},
			},
		},
		Notifiers: NotifiersConfig{
			Slack: []SlackInstance{
				{
					Name:    "team-slack",
					Enabled: true,
					Auth:    &SlackAuth{WebhookURLFile: testSlackWebhookURL},
					When:    `event.State == "failed"`,
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	if len(errs) != 0 {
		t.Errorf("Expected no errors, got %d:", len(errs))
		for _, e := range errs {
			t.Errorf("  %s", e.Error())
		}
	}
}

func TestValidateAll_MissingRequired(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: ""},
	}

	errs := ValidateAll(cfg)
	found := false
	for _, e := range errs {
		if e.Path == "server.addr" && e.Message == ValidationMsgRequired {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected error for missing server.addr")
	}
}

func TestValidateAll_HMACAuthRequiresTimestampValidation(t *testing.T) {
	// Given: HMAC server auth is enabled without replay timestamp validation.
	cfg := &Config{
		Server: Server{
			Addr: DefaultServerAddr,
			Auth: AuthConfig{ //nolint:gosec // test data
				Enabled:    true,
				Type:       AuthTypeHMACSHA256,
				SecretFile: "/etc/secrets/server/auth/secret",
			},
		},
	}

	// When: all config validation paths are collected.
	errs := ValidateAll(cfg)

	// Then: the error points operators at the unsafe omitted field.
	found := false
	for _, e := range errs {
		if e.Path == "server.auth.validate_timestamp" && e.Message == ValidationMsgHMACReplayRequired {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected HMAC replay validation error, got: %v", errs)
	}
}

func TestValidateAll_InvalidCEL(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		SCM: SCMConfig{
			GitHub: []GitHubInstance{
				{
					Name:    "main",
					Enabled: true,
					Auth:    &GitHubAuth{SecretFile: "token123"},
					BaseURL: "https://github.com",
					Actions: []Action{
						{
							Name: "status",
							Type: notifier.ActionCommitStatus,
							When: "invalid cel +++",
						},
					},
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	found := false
	for _, e := range errs {
		if e.Path == "scm.github[0].actions[0].when" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected error for invalid CEL expression")
	}
}

func TestValidateAll_InvalidTemplate(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		Notifiers: NotifiersConfig{
			Slack: []SlackInstance{
				{
					Name:     "team-slack",
					Enabled:  true,
					Auth:     &SlackAuth{WebhookURLFile: testSlackWebhookURL},
					Template: "{{ .Invalid | bad_func }}",
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	found := false
	for _, e := range errs {
		if e.Path == "notifiers.slack[0].template" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected error for invalid template, got: %v", errs)
	}
}

func TestValidateAll_InvalidActionType(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		SCM: SCMConfig{
			GitHub: []GitHubInstance{
				{
					Name:    "main",
					Enabled: true,
					Auth:    &GitHubAuth{SecretFile: "token123"},
					BaseURL: "https://github.com",
					Actions: []Action{
						{
							Name: "bad",
							Type: "nonexistent_type",
						},
					},
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	found := false
	for _, e := range errs {
		if e.Path == "scm.github[0].actions[0].type" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected error for invalid action type, got: %v", errs)
	}
}

func TestValidateAll_MultipleErrors(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: ""},
		SCM: SCMConfig{
			GitHub: []GitHubInstance{
				{
					Name:    "",
					Enabled: true,
					Auth:    &GitHubAuth{SecretFile: ""},
					BaseURL: "",
					Actions: []Action{
						{
							Name: "bad-cel",
							Type: "invalid",
							When: "broken +++",
						},
					},
				},
			},
		},
		Notifiers: NotifiersConfig{
			Slack: []SlackInstance{
				{
					Name:    "",
					Enabled: true,
					Auth:    nil, // missing auth triggers error
					When:    "also broken +++",
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	// Should have multiple errors collected, not just the first one
	if len(errs) < 5 {
		t.Errorf("Expected at least 5 errors for multiple issues, got %d:", len(errs))
		for _, e := range errs {
			t.Errorf("  %s", e.Error())
		}
	}
}

func TestValidateAll_InvalidURL(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		SCM: SCMConfig{
			GitHub: []GitHubInstance{
				{
					Name:    "main",
					Enabled: true,
					Auth:    &GitHubAuth{SecretFile: "token123"},
					BaseURL: "://invalid",
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	found := false
	for _, e := range errs {
		if e.Path == "scm.github[0].base_url" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected error for invalid URL, got: %v", errs)
	}
}

func TestValidateAll_GitLab(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		SCM: SCMConfig{
			GitLab: []GitLabInstance{
				{
					Name:    "",
					Enabled: true,
					Variant: "", // empty variant should error
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"scm.gitlab[0].name":    false,
		"scm.gitlab[0].auth":    false,
		"scm.gitlab[0].variant": false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s", path)
		}
	}
}

func TestValidateAll_Bitbucket(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		SCM: SCMConfig{
			Bitbucket: []BitbucketInstance{
				{
					Name:    "",
					Enabled: true,
					Variant: "invalid",
					BaseURL: "https://bitbucket.org",
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"scm.bitbucket[0].name":    false,
		"scm.bitbucket[0].variant": false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s", path)
		}
	}
}

func TestValidateAll_Azure(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		SCM: SCMConfig{
			Azure: []AzureInstance{
				{
					Name:       "",
					Enabled:    true,
					SecretFile: "",
					BaseURL:    "",
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"scm.azure_devops[0].name":        false,
		"scm.azure_devops[0].secret_file": false,
		"scm.azure_devops[0].base_url":    false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s", path)
		}
	}
}

func TestValidateAll_Gitea(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		SCM: SCMConfig{
			Gitea: []GiteaInstance{
				{
					Name:    "",
					Enabled: true,
					BaseURL: "",
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"scm.gitea[0].name":     false,
		"scm.gitea[0].auth":     false,
		"scm.gitea[0].base_url": false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s", path)
		}
	}
}

func TestValidateAll_SourceHut(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		SCM: SCMConfig{
			SourceHut: []SourceHutInstance{
				{
					Name:    "",
					Enabled: true,
					BaseURL: "",
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"scm.sourcehut[0].name":     false,
		"scm.sourcehut[0].auth":     false,
		"scm.sourcehut[0].base_url": false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s", path)
		}
	}
}

func TestValidateAll_Slack_missing_auth(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		Notifiers: NotifiersConfig{
			Slack: []SlackInstance{
				{
					Name:    "",
					Enabled: true,
					Auth:    nil,
					When:    invalidCELExpr,
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"notifiers.slack[0].name": false,
		"notifiers.slack[0].auth": false,
		"notifiers.slack[0].when": false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s, all errors: %v", path, errs)
		}
	}
}

func TestValidateAll_Slack_bot_token_missing_fields(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		Notifiers: NotifiersConfig{
			Slack: []SlackInstance{
				{
					Name:    "bot-slack",
					Enabled: true,
					Auth: &SlackAuth{
						BotToken: &BotTokenAuth{
							// TokenFile and ChannelID missing
						},
					},
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"notifiers.slack[0].auth.bot_token.token_file": false,
		"notifiers.slack[0].auth.bot_token.channel_id": false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s, all errors: %v", path, errs)
		}
	}
}

func TestValidateAll_Teams(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		Notifiers: NotifiersConfig{
			Teams: []TeamsInstance{
				{
					Name:    "",
					Enabled: true,
					Auth:    nil,
					When:    invalidCELExpr,
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"notifiers.teams[0].name": false,
		"notifiers.teams[0].auth": false,
		"notifiers.teams[0].when": false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s", path)
		}
	}
}

func TestValidateAll_Discord(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		Notifiers: NotifiersConfig{
			Discord: []DiscordInstance{
				{
					Name:    "",
					Enabled: true,
					Auth:    nil,
					When:    invalidCELExpr,
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"notifiers.discord[0].name": false,
		"notifiers.discord[0].auth": false,
		"notifiers.discord[0].when": false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s", path)
		}
	}
}

func TestValidateAll_Discord_bot_token_missing_fields(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		Notifiers: NotifiersConfig{
			Discord: []DiscordInstance{
				{
					Name:    "bot-discord",
					Enabled: true,
					Auth: &DiscordAuth{
						BotToken: &BotTokenAuth{
							// TokenFile and ChannelID missing
						},
					},
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"notifiers.discord[0].auth.bot_token.token_file": false,
		"notifiers.discord[0].auth.bot_token.channel_id": false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s, all errors: %v", path, errs)
		}
	}
}

func TestValidateAll_PagerDuty(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		Notifiers: NotifiersConfig{
			PagerDuty: []PagerDutyInstance{
				{
					Name:    "",
					Enabled: true,
					Auth:    nil,
					When:    invalidCELExpr,
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"notifiers.pagerduty[0].name": false,
		"notifiers.pagerduty[0].auth": false,
		"notifiers.pagerduty[0].when": false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s", path)
		}
	}
}

func TestValidateAll_Datadog(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		Notifiers: NotifiersConfig{
			Datadog: []DatadogInstance{
				{
					Name:    "",
					Enabled: true,
					Auth:    nil,
					When:    invalidCELExpr,
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"notifiers.datadog[0].name": false,
		"notifiers.datadog[0].auth": false,
		"notifiers.datadog[0].when": false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s", path)
		}
	}
}

func TestValidateAll_Webhook(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		Notifiers: NotifiersConfig{
			Webhook: []WebhookInstance{
				{
					Name:    "",
					Enabled: true,
					URLFile: "",
					When:    invalidCELExpr,
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	expectedErrors := map[string]bool{
		"notifiers.webhook[0].name":     false,
		"notifiers.webhook[0].url_file": false,
		"notifiers.webhook[0].when":     false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected error for %s", path)
		}
	}
}

func TestValidate_Category1TemplatesRequired(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		Notifiers: NotifiersConfig{
			Email: []EmailInstance{
				{
					Name:     "email-empty",
					Enabled:  true,
					Host:     "smtp.example.com",
					From:     testEmailFrom,
					To:       []string{"team@example.com"},
					Subject:  "",
					Template: "",
				},
			},
			Grafana: []GrafanaInstance{
				{
					Name:     "grafana-empty",
					Enabled:  true,
					URL:      "https://grafana.example.com",
					Auth:     &GrafanaAuth{TokenFile: "/etc/secrets/grafana/token"}, //nolint:gosec // test data
					Template: "",
				},
			},
		},
		Jira: []JiraInstance{
			{
				Name:    "jira-empty",
				Enabled: true,
				BaseURL: testBaseURLJira,
				Auth:    &JiraAuth{TokenFile: "/etc/secrets/jira/token"}, //nolint:gosec // test data
				Actions: []JiraAction{
					{
						Name:     "comment-empty",
						Type:     JiraActionComment,
						Enabled:  true,
						Template: "",
					},
				},
			},
		},
	}

	errs := ValidateAll(cfg)

	expectedErrors := map[string]bool{
		"notifiers.email[0].subject":    false,
		"notifiers.email[0].template":   false,
		"notifiers.grafana[0].template": false,
		"jira[0].actions[0].template":   false,
	}

	for _, e := range errs {
		if _, ok := expectedErrors[e.Path]; ok {
			expectedErrors[e.Path] = true
		}
	}

	for path, found := range expectedErrors {
		if !found {
			t.Errorf("Expected Category-1 required error for %s, all errors: %v", path, errs)
		}
	}
}

func TestValidateAll_ContextPerTaskRequiresCommitStatus(t *testing.T) {
	tests := []struct {
		name       string
		actionType ActionType
		wantErr    bool
	}{
		{
			name:       "pr_comment with context_per_task errors",
			actionType: notifier.ActionPRComment,
			wantErr:    true,
		},
		{
			name:       "commit_status with context_per_task is valid",
			actionType: notifier.ActionCommitStatus,
			wantErr:    false,
		},
		{
			name:       "issue_comment with context_per_task errors",
			actionType: notifier.ActionIssueComment,
			wantErr:    true,
		},
		{
			name:       "check_run with context_per_task errors",
			actionType: notifier.ActionCheckRun,
			wantErr:    true,
		},
		{
			name:       "label with context_per_task errors",
			actionType: notifier.ActionLabel,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := Action{
				Name:           "test-action",
				Type:           tt.actionType,
				Enabled:        true,
				ContextPerTask: true,
			}
			if tt.actionType == notifier.ActionLabel {
				action.Labels = &ActionLabels{
					Add: []LabelEntry{{Name: "ci:test"}},
				}
			}

			cfg := &Config{
				Server: Server{Addr: DefaultServerAddr},
				SCM: SCMConfig{
					GitHub: []GitHubInstance{
						{
							Name:    "main",
							Enabled: true,
							Auth:    &GitHubAuth{SecretFile: "token123"},
							BaseURL: "https://github.com",
							Actions: []Action{action},
						},
					},
				},
			}

			errs := ValidateAll(cfg)
			found := false
			for _, e := range errs {
				if e.Path == "scm.github[0].actions[0].context_per_task" {
					found = true
					if !strings.Contains(e.Message, "context_per_task is only valid for commit_status") {
						t.Errorf("unexpected message: %s", e.Message)
					}
					break
				}
			}

			if tt.wantErr && !found {
				t.Errorf("expected context_per_task error for type %q, got errors: %v", tt.actionType, errs)
			}
			if !tt.wantErr && found {
				t.Errorf("unexpected context_per_task error for type %q: %v", tt.actionType, errs)
			}
		})
	}
}

func TestValidateAll_ContextPerTaskWithCommitStatus_Valid(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		SCM: SCMConfig{
			GitHub: []GitHubInstance{
				{
					Name:    "main",
					Enabled: true,
					Auth:    &GitHubAuth{SecretFile: "token123"},
					BaseURL: "https://github.com",
					Actions: []Action{
						{
							Name:           "status",
							Type:           notifier.ActionCommitStatus,
							Enabled:        true,
							ContextPerTask: true,
						},
					},
				},
			},
		},
	}

	errs := ValidateAll(cfg)
	for _, e := range errs {
		if e.Path == "scm.github[0].actions[0].context_per_task" {
			t.Errorf("commit_status with context_per_task should not error: %s", e.Error())
		}
	}
}

func TestValidateAll_NegativeNumericRanges(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		errPath  string
		errWords string
	}{
		{
			name: "negative handler_timeout",
			cfg: &Config{
				Server:         Server{Addr: DefaultServerAddr},
				HandlerTimeout: -1,
			},
			errPath:  "handler_timeout",
			errWords: "non-negative",
		},
		{
			name: "negative max_concurrency",
			cfg: &Config{
				Server:         Server{Addr: DefaultServerAddr},
				MaxConcurrency: -5,
			},
			errPath:  "max_concurrency",
			errWords: "non-negative",
		},
		{
			name: "negative retry.max_attempts",
			cfg: &Config{
				Server: Server{Addr: DefaultServerAddr},
				Retry:  RetryConfig{MaxAttempts: -1},
			},
			errPath:  "retry.max_attempts",
			errWords: "non-negative",
		},
		{
			name: "negative dedupe_size",
			cfg: &Config{
				Server:     Server{Addr: DefaultServerAddr},
				DedupeSize: -100,
			},
			errPath:  "dedupe_size",
			errWords: "non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateAll(tt.cfg)
			found := false
			for _, e := range errs {
				if e.Path == tt.errPath {
					found = true
					if !strings.Contains(e.Message, tt.errWords) {
						t.Errorf("error message should contain %q, got: %s", tt.errWords, e.Message)
					}
					break
				}
			}
			if !found {
				t.Errorf("expected error at path %q, got errors: %v", tt.errPath, errs)
			}
		})
	}
}

func TestValidateAll_ZeroValuesAllowed(t *testing.T) {
	cfg := &Config{
		Server:         Server{Addr: DefaultServerAddr},
		HandlerTimeout: 0,
		MaxConcurrency: 0,
		DedupeSize:     0,
	}

	errs := ValidateAll(cfg)
	for _, e := range errs {
		if e.Path == "handler_timeout" || e.Path == "max_concurrency" || e.Path == "dedupe_size" {
			t.Errorf("zero value should be allowed at path %q: %s", e.Path, e.Error())
		}
	}
}

func TestValidate_Category2EmptyTemplatesAllowed(t *testing.T) {
	cfg := &Config{
		Server: Server{Addr: DefaultServerAddr},
		SCM: SCMConfig{
			GitHub: []GitHubInstance{
				{
					Name:    "gh",
					Enabled: true,
					Auth:    &GitHubAuth{SecretFile: testTokenShort},
					BaseURL: "https://github.com",
					Actions: []Action{
						{
							Name:     "pr-comment",
							Type:     notifier.ActionPRComment,
							Enabled:  true,
							Template: "",
						},
					},
				},
			},
		},
		Notifiers: NotifiersConfig{
			Slack: []SlackInstance{
				{
					Name:     "slack-empty",
					Enabled:  true,
					Auth:     &SlackAuth{WebhookURLFile: testSlackWebhookURL},
					Template: "",
				},
			},
			Teams: []TeamsInstance{
				{
					Name:     "teams-empty",
					Enabled:  true,
					Auth:     &TeamsAuth{WebhookURLFile: "https://webhook.office.com/webhook"},
					Template: "",
				},
			},
			Discord: []DiscordInstance{
				{
					Name:     "discord-empty",
					Enabled:  true,
					Auth:     &DiscordAuth{WebhookURLFile: "https://discord.com/api/webhooks/123"},
					Template: "",
				},
			},
			Webhook: []WebhookInstance{
				{
					Name:    "webhook-empty",
					Enabled: true,
					URLFile: "/etc/secrets/webhook/url",
				},
			},
		},
	}

	errs := ValidateAll(cfg)

	category2Paths := []string{
		"notifiers.slack[0].template",
		"notifiers.teams[0].template",
		"notifiers.discord[0].template",
		"scm.github[0].actions[0].template",
	}

	for _, e := range errs {
		for _, p := range category2Paths {
			if e.Path == p {
				t.Errorf("Category-2 empty template should not error: %s: %s", e.Path, e.Message)
			}
		}
	}
}
