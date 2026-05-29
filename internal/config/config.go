// Package config provides configuration loading and management for tekton-events-relay.
package config

import (
	"fmt"
	"os"
	"regexp"

	"github.com/BurntSushi/toml"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// Config represents the application configuration loaded from TOML.
type Config struct {
	Server       Server       `toml:"server"`
	DashboardURL string       `toml:"dashboard_url"`
	Filter       FilterConfig `toml:"filter"`
	DedupeSize   int          `toml:"dedupe_size"`
	Notifiers    Notifiers    `toml:"notifiers"`
	Logging      LoggingConfig `toml:"logging"`
	Debug        DebugConfig   `toml:"debug"`
}

// Server contains HTTP server configuration.
type Server struct {
	Addr            string `toml:"addr"`
	ReadTimeoutSec  int    `toml:"read_timeout_sec"`
	WriteTimeoutSec int    `toml:"write_timeout_sec"`
}

// FilterConfig controls which event types are processed.
type FilterConfig struct {
	AllowTaskRun     bool `toml:"allow_taskrun"`
	AllowPipelineRun bool `toml:"allow_pipelinerun"`
	IgnoreUnknown    bool `toml:"ignore_unknown"`
}

// Notifiers contains all available notifier configurations.
type Notifiers struct {
	// SCM — commit status
	GitHub          *GitHubConfig          `toml:"github,omitempty"`
	GitLabCloud     *GitLabConfig          `toml:"gitlab_cloud,omitempty"`
	GitLabServer    *GitLabConfig          `toml:"gitlab_server,omitempty"`
	BitbucketCloud  *BitbucketCloudConfig  `toml:"bitbucket_cloud,omitempty"`
	BitbucketServer *BitbucketServerConfig `toml:"bitbucket_server,omitempty"`
	AzureDevOps     *AzureConfig           `toml:"azure_devops,omitempty"`
	Gitea           *GiteaConfig           `toml:"gitea,omitempty"`
	SourceHut       *SourceHutConfig       `toml:"sourcehut,omitempty"`
	// Chat
	Slack   *SlackConfig   `toml:"slack,omitempty"`
	Teams   *TeamsConfig   `toml:"teams,omitempty"`
	Discord *DiscordConfig `toml:"discord,omitempty"`
	// Alerting / Observability
	PagerDuty *PagerDutyConfig `toml:"pagerduty,omitempty"`
	Datadog   *DatadogConfig   `toml:"datadog,omitempty"`
	// Generic
	Webhook *WebhookConfig `toml:"webhook,omitempty"`
}

// ActionCommentConfig configures comment actions (PR/issue comments).
type ActionCommentConfig struct {
	Enabled  bool           `toml:"enabled"`
	Template string         `toml:"template"`
	OnStates []domain.State `toml:"on_states"`
	When     string         `toml:"when"`
}

// ActionLabelConfig configures label actions.
type ActionLabelConfig struct {
	Enabled      bool   `toml:"enabled"`
	SuccessLabel string `toml:"success_label"`
	FailureLabel string `toml:"failure_label"`
	When         string `toml:"when"`
}

// GitHubActionsConfig contains GitHub action handler configurations.
type GitHubActionsConfig struct {
	PRComment    *ActionCommentConfig `toml:"pr_comment,omitempty"`
	IssueComment *ActionCommentConfig `toml:"issue_comment,omitempty"`
	Label        *ActionLabelConfig   `toml:"label,omitempty"`
}

// GiteaActionsConfig contains Gitea action handler configurations.
type GiteaActionsConfig struct {
	PRComment    *ActionCommentConfig `toml:"pr_comment,omitempty"`
	IssueComment *ActionCommentConfig `toml:"issue_comment,omitempty"`
	Label        *ActionLabelConfig   `toml:"label,omitempty"`
}

// GitLabActionsConfig contains GitLab action handler configurations.
type GitLabActionsConfig struct {
	Label *ActionLabelConfig `toml:"label,omitempty"`
}

// AzureActionsConfig contains Azure DevOps action handler configurations.
type AzureActionsConfig struct {
	Label *ActionLabelConfig `toml:"label,omitempty"`
}

// GitHubConfig contains GitHub notifier settings.
type GitHubConfig struct {
	Enabled            bool                 `toml:"enabled"`
	Token              string               `toml:"token"`
	BaseURL            string               `toml:"base_url"`
	InsecureSkipVerify bool                 `toml:"insecure_skip_verify"`
	Actions            *GitHubActionsConfig `toml:"actions,omitempty"`
}

// GitLabConfig contains GitLab notifier settings.
type GitLabConfig struct {
	Enabled            bool                 `toml:"enabled"`
	Token              string               `toml:"token"`
	BaseURL            string               `toml:"base_url"`
	InsecureSkipVerify bool                 `toml:"insecure_skip_verify"`
	Actions            *GitLabActionsConfig `toml:"actions,omitempty"`
}

// BitbucketCloudConfig contains Bitbucket Cloud notifier settings.
type BitbucketCloudConfig struct {
	Enabled            bool   `toml:"enabled"`
	Username           string `toml:"username"`
	AppPassword        string `toml:"app_password"`
	BaseURL            string `toml:"base_url"`
	InsecureSkipVerify bool   `toml:"insecure_skip_verify"`
}

// BitbucketServerConfig contains Bitbucket Server notifier settings.
type BitbucketServerConfig struct {
	Enabled            bool   `toml:"enabled"`
	Token              string `toml:"token"`
	BaseURL            string `toml:"base_url"`
	InsecureSkipVerify bool   `toml:"insecure_skip_verify"`
}

// AzureConfig contains Azure DevOps notifier settings.
type AzureConfig struct {
	Enabled            bool                `toml:"enabled"`
	Token              string              `toml:"token"`
	BaseURL            string              `toml:"base_url"`
	Genre              string              `toml:"genre"`
	InsecureSkipVerify bool                `toml:"insecure_skip_verify"`
	Actions            *AzureActionsConfig `toml:"actions,omitempty"`
}

// GiteaConfig contains Gitea notifier settings.
type GiteaConfig struct {
	Enabled            bool                `toml:"enabled"`
	Token              string              `toml:"token"`
	BaseURL            string              `toml:"base_url"`
	InsecureSkipVerify bool                `toml:"insecure_skip_verify"`
	Actions            *GiteaActionsConfig `toml:"actions,omitempty"`
}

// SourceHutConfig contains SourceHut notifier settings.
type SourceHutConfig struct {
	Enabled            bool   `toml:"enabled"`
	Token              string `toml:"token"`
	BaseURL            string `toml:"base_url"`
	InsecureSkipVerify bool   `toml:"insecure_skip_verify"`
}

// SlackConfig contains Slack notifier settings.
type SlackConfig struct {
	Enabled    bool     `toml:"enabled"`
	WebhookURL string   `toml:"webhook_url"`
	Channel    string   `toml:"channel"`
	Username   string   `toml:"username"`
	IconEmoji  string   `toml:"icon_emoji"`
	NotifyOn   []string `toml:"notify_on"`
}

// TeamsConfig contains Microsoft Teams notifier settings.
type TeamsConfig struct {
	Enabled    bool     `toml:"enabled"`
	WebhookURL string   `toml:"webhook_url"`
	NotifyOn   []string `toml:"notify_on"`
}

// DiscordConfig contains Discord notifier settings.
type DiscordConfig struct {
	Enabled    bool     `toml:"enabled"`
	WebhookURL string   `toml:"webhook_url"`
	Username   string   `toml:"username"`
	NotifyOn   []string `toml:"notify_on"`
}

// PagerDutyConfig contains PagerDuty notifier settings.
type PagerDutyConfig struct {
	Enabled        bool   `toml:"enabled"`
	IntegrationKey string `toml:"integration_key"`
	Severity       string `toml:"severity"`
}

// DatadogConfig contains Datadog notifier settings.
type DatadogConfig struct {
	Enabled  bool     `toml:"enabled"`
	APIKey   string   `toml:"api_key"`
	Site     string   `toml:"site"`
	Tags     []string `toml:"tags"`
	NotifyOn []string `toml:"notify_on"`
}

// WebhookConfig contains generic webhook notifier settings.
type WebhookConfig struct {
	Enabled  bool              `toml:"enabled"`
	URL      string            `toml:"url"`
	Headers  map[string]string `toml:"headers"`
	NotifyOn []string          `toml:"notify_on"`
}

// LoggingConfig contains logging configuration.
type LoggingConfig struct {
	Level string `toml:"level"` // debug, info, warn, error
}

// DebugConfig contains debug mode configuration.
type DebugConfig struct {
	Enabled       bool `toml:"enabled"`
	LogPayloads   bool `toml:"log_payloads"`
	LogHTTPCalls  bool `toml:"log_http_calls"`
}

// Load reads and parses the configuration file at the given path.
func Load(path string) (*Config, error) {
	// #nosec G304 -- Config file path from validated config
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	expanded := expandEnv(string(raw))

	var cfg Config
	if err := toml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse toml: %w", err)
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

var envRe = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

func expandEnv(s string) string {
	return envRe.ReplaceAllStringFunc(s, func(match string) string {
		return os.Getenv(match[2 : len(match)-1])
	})
}

func applyDefaults(c *Config) {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Server.ReadTimeoutSec == 0 {
		c.Server.ReadTimeoutSec = 10
	}
	if c.Server.WriteTimeoutSec == 0 {
		c.Server.WriteTimeoutSec = 10
	}
	if c.DedupeSize == 0 {
		c.DedupeSize = 10000
	}
	if !c.Filter.AllowTaskRun && !c.Filter.AllowPipelineRun {
		c.Filter.AllowPipelineRun = true
		c.Filter.IgnoreUnknown = true
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
}
