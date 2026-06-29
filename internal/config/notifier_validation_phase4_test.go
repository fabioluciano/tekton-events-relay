package config

import (
	"strings"
	"testing"
)

const (
	phase4Template          = "{{.RunName}}"
	phase4WebhookAuthPrefix = "notifiers.webhook[0].auth"
)

func TestValidate_Phase4NotifierAuthRules_rejectInvalidConfigs(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		wantPath string
		wantMsg  string
	}{
		{
			name: "slack requires exactly one auth mode",
			cfg: &Config{Notifiers: NotifiersConfig{Slack: []SlackInstance{{
				Name:    "slack",
				Enabled: true,
				Auth:    &SlackAuth{},
			}}}},
			wantPath: "notifiers.slack[0].auth",
			wantMsg:  "exactly one",
		},
		{
			name: "discord rejects webhook and bot token together",
			cfg: &Config{Notifiers: NotifiersConfig{Discord: []DiscordInstance{{
				Name:    "discord",
				Enabled: true,
				Auth: &DiscordAuth{
					WebhookURLFile: "/etc/secrets/discord/main/webhook_url",
					BotToken:       &BotTokenAuth{TokenFile: "/etc/secrets/discord/main/token", ChannelID: "123"}, //nolint:gosec // G101: test fixture path, not a credential
				},
			}}}},
			wantPath: "notifiers.discord[0].auth",
			wantMsg:  "mutually exclusive",
		},
		{
			name: "grafana requires url",
			cfg: &Config{Notifiers: NotifiersConfig{Grafana: []GrafanaInstance{{
				Name:     "grafana",
				Enabled:  true,
				Auth:     &GrafanaAuth{TokenFile: "/etc/secrets/grafana/main/token"}, //nolint:gosec // G101: test fixture path, not a credential
				Template: phase4Template,
			}}}},
			wantPath: "notifiers.grafana[0].url",
			wantMsg:  ValidationMsgBaseURLRequired,
		},
		{
			name: "grafana requires token auth",
			cfg: &Config{Notifiers: NotifiersConfig{Grafana: []GrafanaInstance{{
				Name:     "grafana",
				Enabled:  true,
				URL:      "https://grafana.example.com",
				Template: phase4Template,
			}}}},
			wantPath: "notifiers.grafana[0].auth",
			wantMsg:  ValidationMsgAuthRequired,
		},
		{
			name: "sentry requires org",
			cfg: &Config{Notifiers: NotifiersConfig{Sentry: []SentryInstance{{
				Name:    "sentry",
				Enabled: true,
				Auth:    &SentryAuth{TokenFile: "/etc/secrets/sentry/main/token"}, //nolint:gosec // G101: test fixture path, not a credential
			}}}},
			wantPath: "notifiers.sentry[0].org",
			wantMsg:  ValidationMsgRequired,
		},
		{
			name: "sentry requires token auth",
			cfg: &Config{Notifiers: NotifiersConfig{Sentry: []SentryInstance{{
				Name:    "sentry",
				Enabled: true,
				Org:     "acme",
			}}}},
			wantPath: "notifiers.sentry[0].auth",
			wantMsg:  ValidationMsgAuthRequired,
		},
		{
			name: "email username requires password file",
			cfg: &Config{Notifiers: NotifiersConfig{Email: []EmailInstance{{
				Name:     "email",
				Enabled:  true,
				Host:     "smtp.example.com",
				From:     testEmailFrom,
				To:       []string{"team@example.com"},
				Subject:  phase4Template,
				Template: phase4Template,
				Auth:     &EmailAuth{Username: testEmailFrom},
			}}}},
			wantPath: "notifiers.email[0].auth.password_file",
			wantMsg:  "password_file required",
		},
		{
			name: "email xoauth2 requires token file",
			cfg: &Config{Notifiers: NotifiersConfig{Email: []EmailInstance{{
				Name:     "email",
				Enabled:  true,
				Host:     "smtp.example.com",
				From:     testEmailFrom,
				To:       []string{"team@example.com"},
				Subject:  phase4Template,
				Template: phase4Template,
				Auth:     &EmailAuth{Username: testEmailFrom, XOAuth2: true},
			}}}},
			wantPath: "notifiers.email[0].auth.token_file",
			wantMsg:  "token_file required",
		},
		{
			name: "jira requires token without oauth2",
			cfg: &Config{Jira: []JiraInstance{{
				Name:    "jira",
				Enabled: true,
				BaseURL: testBaseURLJira,
				Auth:    &JiraAuth{},
			}}},
			wantPath: "jira[0].auth.token_file",
			wantMsg:  ValidationMsgRequiredForEnabled,
		},
		{
			name: "webhook requires url file",
			cfg: &Config{Notifiers: NotifiersConfig{Webhook: []WebhookInstance{{
				Name:    "webhook",
				Enabled: true,
			}}}},
			wantPath: "notifiers.webhook[0].url_file",
			wantMsg:  "url_file",
		},
		{
			name:     "webhook bearer requires token file",
			cfg:      phase4WebhookConfig(&WebhookAuthConfig{Type: "bearer"}),
			wantPath: phase4WebhookAuthPrefix,
			wantMsg:  "type 'bearer' requires 'token_file'",
		},
		{
			name:     "webhook basic requires username and password files",
			cfg:      phase4WebhookConfig(&WebhookAuthConfig{Type: "basic", UsernameFile: "/etc/secrets/webhook/main/username"}),
			wantPath: phase4WebhookAuthPrefix,
			wantMsg:  "type 'basic' requires 'username_file' and 'password_file'",
		},
		{
			name:     "webhook apikey requires header",
			cfg:      phase4WebhookConfig(&WebhookAuthConfig{Type: "apikey", TokenFile: "/etc/secrets/webhook/main/token"}), //nolint:gosec // G101: test fixture path, not a credential
			wantPath: phase4WebhookAuthPrefix,
			wantMsg:  "type 'apikey' requires 'token_file' and 'header'",
		},
		{
			name:     "webhook hmac requires secret file",
			cfg:      phase4WebhookConfig(&WebhookAuthConfig{Type: "hmac"}),
			wantPath: phase4WebhookAuthPrefix,
			wantMsg:  "type 'hmac' requires 'secret_file'",
		},
		{
			name:     "webhook oauth2 requires oauth2 block",
			cfg:      phase4WebhookConfig(&WebhookAuthConfig{Type: testAuthTypeOAuth2}),
			wantPath: phase4WebhookAuthPrefix,
			wantMsg:  "type 'oauth2' requires an 'oauth2' block",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When: runtime config validation evaluates notifier-specific auth rules.
			errs := ValidateAll(tt.cfg)

			// Then: the exact path and actionable message are present.
			if !phase4HasValidationError(errs, tt.wantPath, tt.wantMsg) {
				t.Fatalf("ValidateAll() errors = %v, want path %q containing %q", errs, tt.wantPath, tt.wantMsg)
			}
		})
	}
}

func phase4WebhookConfig(auth *WebhookAuthConfig) *Config {
	return &Config{Notifiers: NotifiersConfig{Webhook: []WebhookInstance{{
		Name:    "webhook",
		Enabled: true,
		URLFile: "/etc/secrets/webhook/main/url",
		Auth:    auth,
	}}}}
}

func phase4HasValidationError(errs []ValidationError, path, message string) bool {
	for _, err := range errs {
		if err.Path == path && strings.Contains(err.Message, message) {
			return true
		}
	}
	return false
}
