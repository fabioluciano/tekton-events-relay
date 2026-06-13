package config

import (
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
					Auth:    &SlackAuth{WebhookURLFile: "https://hooks.slack.com/webhook"},
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
					Auth:     &SlackAuth{WebhookURLFile: "https://hooks.slack.com/webhook"},
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
