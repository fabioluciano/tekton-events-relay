// Package config provides configuration loading and management for tekton-events-relay.
package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// AccumulatorProviderConfig identifies which registered handler the accumulator
// should delegate PR comments to.
type AccumulatorProviderConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"` // "github" | "gitlab" | "gitea"
}

// AccumulatorConfig controls the F3 accumulator feature for aggregating
// TaskRun events into pipeline summaries.
type AccumulatorConfig struct {
	Enabled  bool                       `yaml:"enabled" json:"enabled"`
	TTL      time.Duration              `yaml:"ttl" json:"ttl"`
	MaxSize  int                        `yaml:"max_size" json:"max_size"`
	Template string                     `yaml:"template,omitempty" json:"template,omitempty"`
	Provider *AccumulatorProviderConfig `yaml:"provider,omitempty" json:"provider,omitempty"`
}

// RetryConfig controls outbound HTTP retry behavior for all notifiers
// and SCM clients (exponential backoff with jitter, Retry-After aware).
type RetryConfig struct {
	MaxAttempts    int           `yaml:"max_attempts" json:"max_attempts"`       // total attempts including the first
	InitialBackoff time.Duration `yaml:"initial_backoff" json:"initial_backoff"` // first backoff delay
	MaxBackoff     time.Duration `yaml:"max_backoff" json:"max_backoff"`         // backoff ceiling
}

// StoreConfig selects the state backend shared by the deduper and the
// accumulator. The default in-memory backend is per-pod: state is lost on
// restart and not shared between replicas, so deduplication and accumulation
// are only fully reliable with a single replica. Use valkey or olric to run
// multiple replicas correctly.
type StoreConfig struct {
	Backend string        `yaml:"backend" json:"backend" validate:"omitempty,oneof=memory valkey olric"` // memory | valkey | olric
	TTL     time.Duration `yaml:"ttl,omitempty" json:"ttl,omitempty"`                                    // entry lifetime on remote backends
	Valkey  ValkeyConfig  `yaml:"valkey,omitempty" json:"valkey,omitempty"`
	Olric   OlricConfig   `yaml:"olric,omitempty" json:"olric,omitempty"`
}

// ValkeyConfig configures the valkey backend (any RESP-compatible server).
type ValkeyConfig struct {
	Address      string `yaml:"address" json:"address"`
	PasswordFile string `yaml:"password_file,omitempty" json:"password_file,omitempty"`
	DB           int    `yaml:"db,omitempty" json:"db,omitempty"`
	KeyPrefix    string `yaml:"key_prefix,omitempty" json:"key_prefix,omitempty"`
}

// OlricConfig configures the embedded olric backend. Relay pods form a
// distributed cache between themselves via memberlist gossip.
type OlricConfig struct {
	BindAddr       string   `yaml:"bind_addr,omitempty" json:"bind_addr,omitempty"`
	BindPort       int      `yaml:"bind_port,omitempty" json:"bind_port,omitempty"`
	MemberlistPort int      `yaml:"memberlist_port,omitempty" json:"memberlist_port,omitempty"`
	Peers          []string `yaml:"peers,omitempty" json:"peers,omitempty"` // host:memberlist_port of other relay pods (headless service)
	Env            string   `yaml:"env,omitempty" json:"env,omitempty"`     // memberlist profile: local | lan | wan
}

// DLQConfig controls the dead letter queue for events that failed with
// permanent errors. Disabled by default; when enabled, failed events are
// preserved in a JSONL file and can be inspected and replayed via
// GET /api/v1/dlq and POST /api/v1/dlq/replay.
type DLQConfig struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`
	Path         string `yaml:"path,omitempty" json:"path,omitempty"`
	MaxSizeBytes int64  `yaml:"max_size_bytes,omitempty" json:"max_size_bytes,omitempty"`
}

// Config represents the application configuration loaded from YAML.
type Config struct {
	Server         Server            `yaml:"server" validate:"required"`
	DashboardURL   string            `yaml:"dashboard_url"`
	Filter         FilterConfig      `yaml:"filter"`
	DedupeSize     int               `yaml:"dedupe_size"`
	MaxConcurrency int               `yaml:"max_concurrency"`
	HandlerTimeout time.Duration     `yaml:"handler_timeout,omitempty" json:"handler_timeout,omitempty"` // per-handler execution deadline
	Retry          RetryConfig       `yaml:"retry,omitempty" json:"retry,omitempty"`
	Store          StoreConfig       `yaml:"store,omitempty" json:"store,omitempty"`
	DLQ            DLQConfig         `yaml:"dlq,omitempty" json:"dlq,omitempty"`
	Accumulator    AccumulatorConfig `yaml:"accumulator,omitempty" json:"accumulator,omitempty"`
	SCM            SCMConfig         `yaml:"scm" validate:"required"`
	Notifiers      NotifiersConfig   `yaml:"notifiers" validate:"required"`
	Logging        LoggingConfig     `yaml:"logging"`
	Tracing        TracingConfig     `yaml:"tracing"`
}

// Server contains HTTP server configuration.
type Server struct {
	Addr               string          `yaml:"addr" validate:"required"`
	MetricsAddr        string          `yaml:"metrics_addr"`
	ReadTimeoutSec     int             `yaml:"read_timeout_sec"`
	WriteTimeoutSec    int             `yaml:"write_timeout_sec"`
	ShutdownTimeoutSec int             `yaml:"shutdown_timeout_sec"`
	MaxBodySize        int64           `yaml:"max_body_size"`
	RateLimit          RateLimitConfig `yaml:"rate_limit"`
	Auth               AuthConfig      `yaml:"auth"`
	TLS                ServerTLSConfig `yaml:"tls,omitempty" json:"tls,omitempty"`
}

// ServerTLSConfig enables native TLS on the receiver endpoint. Both files
// must be set; when empty the server speaks plain HTTP (TLS terminated by
// the ingress).
type ServerTLSConfig struct {
	CertFile string `yaml:"cert_file,omitempty" json:"cert_file,omitempty"`
	KeyFile  string `yaml:"key_file,omitempty" json:"key_file,omitempty"`
}

// Enabled reports whether native TLS is configured.
func (t ServerTLSConfig) Enabled() bool { return t.CertFile != "" && t.KeyFile != "" }

// RateLimitConfig holds rate limiting configuration.
type RateLimitConfig struct {
	Enabled           bool    `yaml:"enabled"`
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	Burst             int     `yaml:"burst"`
}

// AuthConfig holds authentication configuration for the server.
type AuthConfig struct {
	Enabled    bool   `yaml:"enabled" validate:"omitempty"`
	Type       string `yaml:"type" validate:"omitempty,oneof=hmac-sha256 bearer"`
	SecretFile string `yaml:"secret_file" validate:"required_if=Enabled true"`

	// ValidateTimestamp enables webhook replay protection: requests must
	// carry an X-Webhook-Timestamp header (unix seconds) within
	// timestamp_tolerance of the server clock. Default tolerance: 5m.
	ValidateTimestamp  bool          `yaml:"validate_timestamp,omitempty" json:"validate_timestamp,omitempty"`
	TimestampTolerance time.Duration `yaml:"timestamp_tolerance,omitempty" json:"timestamp_tolerance,omitempty"`
}

// Defaults for security middleware.
const (
	DefaultMaxBodySize    int64   = 1048576 // 1MB
	DefaultRateLimitRPS   float64 = 100.0
	DefaultRateLimitBurst int     = 200
)

// Common configuration values.
const (
	DefaultServerAddr        = ":8080"
	AuthTypeBearer           = "bearer"
	AuthTypeBasic            = "basic"
	AuthTypeAPIKey           = "apikey"
	AuthTypeHMAC             = "hmac"
	BitbucketVariantCloud    = "cloud"
	BitbucketVariantServer   = "server"
	GitLabVariantSaaS        = "saas"
	GitLabVariantSelfManaged = "self-managed"
)

// Validation error messages.
const (
	ValidationMsgRequired                 = "required"
	ValidationMsgRequiredForEnabled       = "required for enabled instance"
	ValidationMsgRequiredWhenEnabled      = "required when auth is enabled"
	ValidationMsgRequiredForCloudVariant  = "required for cloud variant"
	ValidationMsgRequiredForServerVariant = "required for server variant"
	ValidationPathAuth                    = ".auth"
	ValidationMsgAuthRequired             = "enabled instance missing required field 'auth'"
	ValidationMsgBaseURLRequired          = "enabled instance missing required field 'base_url'"
)

// FilterConfig controls which event types are processed.
type FilterConfig struct {
	AllowTaskRun       bool `yaml:"allow_taskrun"`
	AllowPipelineRun   bool `yaml:"allow_pipelinerun"`
	AllowCustomRun     bool `yaml:"allow_customrun"`
	AllowEventListener bool `yaml:"allow_eventlistener"`
	IgnoreUnknown      bool `yaml:"ignore_unknown"`
}

// SCMConfig contains all SCM provider configurations.
type SCMConfig struct {
	GitHub    []GitHubInstance    `yaml:"github,omitempty" validate:"omitempty,dive"`
	GitLab    []GitLabInstance    `yaml:"gitlab,omitempty" validate:"omitempty,dive"`
	Gitea     []GiteaInstance     `yaml:"gitea,omitempty" validate:"omitempty,dive"`
	Azure     []AzureInstance     `yaml:"azure_devops,omitempty" validate:"omitempty,dive"`
	Bitbucket []BitbucketInstance `yaml:"bitbucket,omitempty" validate:"omitempty,dive"`
	SourceHut []SourceHutInstance `yaml:"sourcehut,omitempty" validate:"omitempty,dive"`
}

// NotifiersConfig contains all generic notifier configurations.
type NotifiersConfig struct {
	Slack     []SlackInstance     `yaml:"slack,omitempty" validate:"omitempty,dive"`
	Teams     []TeamsInstance     `yaml:"teams,omitempty" validate:"omitempty,dive"`
	Discord   []DiscordInstance   `yaml:"discord,omitempty" validate:"omitempty,dive"`
	PagerDuty []PagerDutyInstance `yaml:"pagerduty,omitempty" validate:"omitempty,dive"`
	Datadog   []DatadogInstance   `yaml:"datadog,omitempty" validate:"omitempty,dive"`
	Webhook   []WebhookInstance   `yaml:"webhook,omitempty" validate:"omitempty,dive"`
	Grafana   []GrafanaInstance   `yaml:"grafana,omitempty" validate:"omitempty,dive"`
	Sentry    []SentryInstance    `yaml:"sentry,omitempty" validate:"omitempty,dive"`
	Email     []EmailInstance     `yaml:"email,omitempty" validate:"omitempty,dive"`
}

// ActionType identifies the type of action in the configuration.
type ActionType string

// Action type constants matching notifier.ActionType values.
const (
	ActionTypeCommitStatus      ActionType = "commit_status"
	ActionTypeCommitComment     ActionType = "commit_comment"
	ActionTypePRComment         ActionType = "pr_comment"
	ActionTypeIssueComment      ActionType = "issue_comment"
	ActionTypeLabel             ActionType = "label"
	ActionTypeDiscussionComment ActionType = "discussion_comment"
	ActionTypeCheckRun          ActionType = "check_run"
	ActionTypeDeploymentStatus  ActionType = "deployment_status"
)

// Action represents a single action configuration within an SCM instance.
type Action struct {
	Name    string     `yaml:"name"`
	Type    ActionType `yaml:"type" validate:"required"`
	Enabled bool       `yaml:"enabled"`
	When    string     `yaml:"when,omitempty"`

	// Mode controls comment actions: "create" (default) posts a new comment
	// per event; "upsert" embeds an invisible marker and edits the existing
	// comment for the same run, making the action idempotent.
	Mode string `yaml:"mode,omitempty" validate:"omitempty,oneof=create upsert"`

	// Comment action fields
	// Template is an inline Go text/template for formatting comments on PR/issue/discussion comments and webhook payloads.
	// It is executed with the event object as context, allowing template functions like {{.State}}, {{.RunName}}, etc.
	// For large templates, consider storing the template content in a ConfigMap and mounting it as a volume.
	Template string `yaml:"template,omitempty"`

	// ContextPerTask (commit_status only): TaskRun events post their status
	// under "<context>/<task>" instead of the shared context, yielding one
	// independent check per task.
	ContextPerTask bool `yaml:"context_per_task,omitempty"`

	// Labels declares the label effect (add/remove lists, Go-templated).
	// The action's `when` expression is the only execution gate.
	Labels *ActionLabels `yaml:"labels,omitempty"`

	// Filter
	Filter *ActionFilterConfig `yaml:"filter,omitempty"`
}

// ActionLabels declares labels to add and remove when a label action fires.
// Entries support Go templates evaluated against the event.
// Supports both old string format and new object format with optional color.
type ActionLabels struct {
	Add    []LabelEntry `yaml:"add,omitempty"`
	Remove []LabelEntry `yaml:"remove,omitempty"`
}

// LabelEntry represents a label with optional color.
// UnmarshalYAML allows both string (backward compat) and object format.
type LabelEntry struct {
	Name  string
	Color string
}

// UnmarshalYAML implements yaml.Unmarshaler for backward compatibility.
// Accepts both "labelname" (string) and {name: "labelname", color: "hex"} (object).
func (l *LabelEntry) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try object format first
	var obj struct {
		Name  string `yaml:"name"`
		Color string `yaml:"color,omitempty"`
	}
	if err := unmarshal(&obj); err == nil && obj.Name != "" {
		l.Name = obj.Name
		l.Color = obj.Color
		return nil
	}

	// Fall back to string format (backward compat)
	var str string
	if err := unmarshal(&str); err != nil {
		return err
	}
	l.Name = str
	l.Color = ""
	return nil
}

// ActionFilterConfig configures action-level filtering with allow/deny lists.
type ActionFilterConfig struct {
	Tasks          FilterList `yaml:"tasks,omitempty"`
	Pipelines      FilterList `yaml:"pipelines,omitempty"`
	CustomRuns     FilterList `yaml:"custom_runs,omitempty"`
	EventListeners FilterList `yaml:"event_listeners,omitempty"`
}

// FilterList defines allow and deny lists for filtering.
type FilterList struct {
	Allow []string `yaml:"allow,omitempty"`
	Deny  []string `yaml:"deny,omitempty"`
}

// BotTokenAuth holds bot token authentication for Slack or Discord.
type BotTokenAuth struct {
	TokenFile string `yaml:"token_file"`
	TokenKey  string `yaml:"token_key,omitempty"`
	ChannelID string `yaml:"channel_id"` // required: Slack channel ID / Discord channel snowflake ID
}

// SlackAuth holds authentication configuration for Slack notifiers.
type SlackAuth struct {
	WebhookURLFile string        `yaml:"webhook_url_file,omitempty"`
	WebhookURLKey  string        `yaml:"webhook_url_key,omitempty"`
	BotToken       *BotTokenAuth `yaml:"bot_token,omitempty"`
}

// DiscordAuth holds authentication configuration for Discord notifiers.
type DiscordAuth struct {
	WebhookURLFile string        `yaml:"webhook_url_file,omitempty"`
	WebhookURLKey  string        `yaml:"webhook_url_key,omitempty"`
	BotToken       *BotTokenAuth `yaml:"bot_token,omitempty"`
}

// TeamsAuth holds authentication configuration for Teams notifiers.
type TeamsAuth struct {
	WebhookURLFile string `yaml:"webhook_url_file"`
	WebhookURLKey  string `yaml:"webhook_url_key,omitempty"`
}

// PagerDutyAuth holds authentication configuration for PagerDuty notifiers.
type PagerDutyAuth struct {
	IntegrationKeyFile string `yaml:"integration_key_file"`
	IntegrationKeyKey  string `yaml:"integration_key_key,omitempty"`
}

// DatadogAuth holds authentication configuration for Datadog notifiers.
type DatadogAuth struct {
	APIKeyFile string `yaml:"api_key_file"`
	APIKeyKey  string `yaml:"api_key_key,omitempty"`
}

// GitHubAuth contains authentication configuration for GitHub.
type GitHubAuth struct {
	SecretFile     string `yaml:"secret_file,omitempty"`
	SecretKey      string `yaml:"secret_key,omitempty"`       // Optional: override default "token"
	AppID          int64  `yaml:"app_id,omitempty"`           // GitHub App authentication
	InstallationID int64  `yaml:"installation_id,omitempty"`  // GitHub App authentication
	PrivateKeyFile string `yaml:"private_key_file,omitempty"` // Path to GitHub App RSA private key PEM
}

// OAuth2ClientCredentials configures OAuth2 client_credentials grant.
type OAuth2ClientCredentials struct {
	ClientIDFile     string `yaml:"client_id_file"`
	ClientIDKey      string `yaml:"client_id_key,omitempty"`
	ClientSecretFile string `yaml:"client_secret_file"`
	ClientSecretKey  string `yaml:"client_secret_key,omitempty"`
	TokenURL         string `yaml:"token_url"`
}

// GitLabAuth contains authentication configuration for GitLab.
type GitLabAuth struct {
	SecretFile string                   `yaml:"secret_file,omitempty"`
	SecretKey  string                   `yaml:"secret_key,omitempty"`
	OAuth2     *OAuth2ClientCredentials `yaml:"oauth2,omitempty"`
}

// GiteaAuth contains authentication configuration for Gitea.
type GiteaAuth struct {
	SecretFile string                   `yaml:"secret_file,omitempty"`
	SecretKey  string                   `yaml:"secret_key,omitempty"`
	OAuth2     *OAuth2ClientCredentials `yaml:"oauth2,omitempty"`
}

// SourceHutAuth contains authentication configuration for SourceHut.
type SourceHutAuth struct {
	SecretFile string `yaml:"secret_file,omitempty"`
	SecretKey  string `yaml:"secret_key,omitempty"`
}

// BitbucketAuth contains authentication configuration for Bitbucket.
//
// Variant-specific fields:
// - cloud: use (username_file + app_password_file) OR oauth2
// - server: use token_file
type BitbucketAuth struct {
	UsernameFile    string                   `yaml:"username_file,omitempty"`
	UsernameKey     string                   `yaml:"username_key,omitempty"`
	AppPasswordFile string                   `yaml:"app_password_file,omitempty"`
	AppPasswordKey  string                   `yaml:"app_password_key,omitempty"`
	TokenFile       string                   `yaml:"token_file,omitempty"`
	TokenKey        string                   `yaml:"token_key,omitempty"`
	OAuth2          *OAuth2ClientCredentials `yaml:"oauth2,omitempty"`
}

// GitHubInstance represents a single GitHub instance configuration.
type GitHubInstance struct {
	Name               string      `yaml:"name" validate:"required"`
	Enabled            bool        `yaml:"enabled"`
	BaseURL            string      `yaml:"base_url"`
	InsecureSkipVerify bool        `yaml:"insecure_skip_verify"`
	Auth               *GitHubAuth `yaml:"auth,omitempty"`
	Actions            []Action    `yaml:"actions,omitempty" validate:"omitempty,dive"`
}

func (g GitHubInstance) isEnabled() bool { return g.Enabled }

// SecretFile returns the path to the secret file for authentication.
func (g GitHubInstance) SecretFile() string {
	if g.Auth != nil {
		return g.Auth.SecretFile
	}
	return ""
}

// GitLabInstance represents a single GitLab instance configuration.
type GitLabInstance struct {
	Name               string      `yaml:"name" validate:"required"`
	Variant            string      `yaml:"variant"` // "saas" or "self-managed"
	Enabled            bool        `yaml:"enabled"`
	Auth               *GitLabAuth `yaml:"auth,omitempty"`
	BaseURL            string      `yaml:"base_url"`
	InsecureSkipVerify bool        `yaml:"insecure_skip_verify"`
	Actions            []Action    `yaml:"actions,omitempty" validate:"omitempty,dive"`
}

func (g GitLabInstance) isEnabled() bool { return g.Enabled }

// SecretFile returns the path to the secret file for authentication.
func (g GitLabInstance) SecretFile() string {
	if g.Auth != nil {
		return g.Auth.SecretFile
	}
	return ""
}

// BitbucketInstance represents a single Bitbucket instance configuration.
type BitbucketInstance struct {
	Name               string         `yaml:"name" validate:"required"`
	Variant            string         `yaml:"variant"` // "cloud" or "server"
	Enabled            bool           `yaml:"enabled"`
	Auth               *BitbucketAuth `yaml:"auth,omitempty"`
	BaseURL            string         `yaml:"base_url"`
	InsecureSkipVerify bool           `yaml:"insecure_skip_verify"`
	Actions            []Action       `yaml:"actions,omitempty" validate:"omitempty,dive"`
}

func (b BitbucketInstance) isEnabled() bool { return b.Enabled }

// AzureInstance represents a single Azure DevOps instance configuration.
type AzureInstance struct {
	Name               string   `yaml:"name" validate:"required"`
	Enabled            bool     `yaml:"enabled"`
	SecretFile         string   `yaml:"secret_file"`
	SecretKey          string   `yaml:"secret_key,omitempty"` // Optional: override default "token"
	BaseURL            string   `yaml:"base_url"`
	Genre              string   `yaml:"genre"`
	InsecureSkipVerify bool     `yaml:"insecure_skip_verify"`
	Actions            []Action `yaml:"actions,omitempty" validate:"omitempty,dive"`
}

func (a AzureInstance) isEnabled() bool { return a.Enabled }

// GiteaInstance represents a single Gitea instance configuration.
type GiteaInstance struct {
	Name               string     `yaml:"name" validate:"required"`
	Enabled            bool       `yaml:"enabled"`
	Auth               *GiteaAuth `yaml:"auth,omitempty"`
	BaseURL            string     `yaml:"base_url"`
	InsecureSkipVerify bool       `yaml:"insecure_skip_verify"`
	Actions            []Action   `yaml:"actions,omitempty" validate:"omitempty,dive"`
}

func (g GiteaInstance) isEnabled() bool { return g.Enabled }

// SecretFile returns the path to the secret file for authentication.
func (g GiteaInstance) SecretFile() string {
	if g.Auth != nil {
		return g.Auth.SecretFile
	}
	return ""
}

// SourceHutInstance represents a single SourceHut instance configuration.
type SourceHutInstance struct {
	Name               string         `yaml:"name" validate:"required"`
	Enabled            bool           `yaml:"enabled"`
	Auth               *SourceHutAuth `yaml:"auth,omitempty"`
	BaseURL            string         `yaml:"base_url"`
	InsecureSkipVerify bool           `yaml:"insecure_skip_verify"`
	Actions            []Action       `yaml:"actions,omitempty" validate:"omitempty,dive"`
}

func (s SourceHutInstance) isEnabled() bool { return s.Enabled }

// SecretFile returns the path to the secret file for authentication.
func (s SourceHutInstance) SecretFile() string {
	if s.Auth != nil {
		return s.Auth.SecretFile
	}
	return ""
}

// SlackInstance represents a single Slack notifier configuration.
type SlackInstance struct {
	Name      string     `yaml:"name" validate:"required"`
	Enabled   bool       `yaml:"enabled"`
	Auth      *SlackAuth `yaml:"auth,omitempty"`
	Channel   string     `yaml:"channel"`
	Username  string     `yaml:"username"`
	IconEmoji string     `yaml:"icon_emoji"`
	When      string     `yaml:"when"`
	Template  string     `yaml:"template,omitempty"`
}

func (s SlackInstance) isEnabled() bool { return s.Enabled }

// WebhookURL returns the path to the webhook URL file.
func (s SlackInstance) WebhookURL() string {
	if s.Auth != nil {
		return s.Auth.WebhookURLFile
	}
	return ""
}

// TeamsInstance represents a single Microsoft Teams notifier configuration.
type TeamsInstance struct {
	Name     string     `yaml:"name" validate:"required"`
	Enabled  bool       `yaml:"enabled"`
	Auth     *TeamsAuth `yaml:"auth,omitempty"`
	When     string     `yaml:"when"`
	Template string     `yaml:"template,omitempty"`
}

func (t TeamsInstance) isEnabled() bool { return t.Enabled }

// WebhookURL returns the path to the webhook URL file.
func (t TeamsInstance) WebhookURL() string {
	if t.Auth != nil {
		return t.Auth.WebhookURLFile
	}
	return ""
}

// DiscordInstance represents a single Discord notifier configuration.
type DiscordInstance struct {
	Name     string       `yaml:"name" validate:"required"`
	Enabled  bool         `yaml:"enabled"`
	Auth     *DiscordAuth `yaml:"auth,omitempty"`
	Username string       `yaml:"username"`
	When     string       `yaml:"when"`
	Template string       `yaml:"template,omitempty"`
}

func (d DiscordInstance) isEnabled() bool { return d.Enabled }

// WebhookURL returns the path to the webhook URL file.
func (d DiscordInstance) WebhookURL() string {
	if d.Auth != nil {
		return d.Auth.WebhookURLFile
	}
	return ""
}

// EmailInstance represents a single SMTP email notifier configuration.
type EmailInstance struct {
	Name    string `yaml:"name" validate:"required"`
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    int    `yaml:"port,omitempty"` // default 587
	// Encryption: starttls (default), tls (implicit, 465) or none (in-cluster relays)
	Encryption string     `yaml:"encryption,omitempty" validate:"omitempty,oneof=starttls tls none"`
	Auth       *EmailAuth `yaml:"auth,omitempty"`
	From       string     `yaml:"from"`
	To         []string   `yaml:"to"`
	// Subject is a Go template rendered against the event (CR/LF stripped).
	Subject string `yaml:"subject,omitempty"`
	// Template is the body Go template; a readable plain-text default applies.
	Template string `yaml:"template,omitempty"`
	HTML     bool   `yaml:"html,omitempty"`
	// InsecureSkipVerify disables TLS verification (self-hosted relays).
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify,omitempty"`
	When               string `yaml:"when"`
}

func (e EmailInstance) isEnabled() bool { return e.Enabled }

// EmailAuth holds SMTP credentials. Password comes from a mounted secret.
type EmailAuth struct {
	Username     string `yaml:"username,omitempty"`
	PasswordFile string `yaml:"password_file,omitempty"`
	PasswordKey  string `yaml:"password_key,omitempty"`
}

// PagerDutyInstance represents a single PagerDuty notifier configuration.
type PagerDutyInstance struct {
	Name     string         `yaml:"name" validate:"required"`
	Enabled  bool           `yaml:"enabled"`
	Auth     *PagerDutyAuth `yaml:"auth,omitempty"`
	Severity string         `yaml:"severity"`
	When     string         `yaml:"when"`
}

func (p PagerDutyInstance) isEnabled() bool { return p.Enabled }

// Template returns an empty string as PagerDuty does not use templates.
func (p PagerDutyInstance) Template() string { return "" }

// DatadogInstance represents a single Datadog notifier configuration.
type DatadogInstance struct {
	Name    string       `yaml:"name" validate:"required"`
	Enabled bool         `yaml:"enabled"`
	Auth    *DatadogAuth `yaml:"auth,omitempty"`
	Site    string       `yaml:"site"`
	Tags    []string     `yaml:"tags"`
	When    string       `yaml:"when"`
}

func (d DatadogInstance) isEnabled() bool { return d.Enabled }

// Template returns an empty string as Datadog does not use templates.
func (d DatadogInstance) Template() string { return "" }

// WebhookInstance represents a single generic webhook notifier configuration.
type WebhookInstance struct {
	Name      string             `yaml:"name" validate:"required"`
	Enabled   bool               `yaml:"enabled"`
	URLFile   string             `yaml:"url_file"`
	URLKey    string             `yaml:"url_key,omitempty"` // Optional: override default "url"
	Auth      *WebhookAuthConfig `yaml:"auth,omitempty"`
	Transform string             `yaml:"transform,omitempty"` // gojq expression to transform payload
	Headers   map[string]string  `yaml:"headers"`
	When      string             `yaml:"when"`
}

func (w WebhookInstance) isEnabled() bool { return w.Enabled }

// WebhookAuthConfig defines authentication configuration for webhook notifiers.
type WebhookAuthConfig struct {
	Type         string `yaml:"type"`                    // bearer, basic, apikey, hmac
	TokenFile    string `yaml:"token_file,omitempty"`    // for bearer and apikey
	UsernameFile string `yaml:"username_file,omitempty"` // for basic
	PasswordFile string `yaml:"password_file,omitempty"` // for basic
	Header       string `yaml:"header,omitempty"`        // for apikey (e.g., "X-API-Key")
	SecretFile   string `yaml:"secret_file,omitempty"`   // for hmac
}

// GrafanaAuth holds authentication configuration for Grafana notifiers.
type GrafanaAuth struct {
	TokenFile string `yaml:"token_file"`
	TokenKey  string `yaml:"token_key,omitempty"`
}

// GrafanaInstance posts deployment markers to the Grafana Annotations API.
type GrafanaInstance struct {
	Name     string       `yaml:"name" validate:"required"`
	Enabled  bool         `yaml:"enabled"`
	URL      string       `yaml:"url"`
	Auth     *GrafanaAuth `yaml:"auth,omitempty"`
	Tags     []string     `yaml:"tags,omitempty"`
	When     string       `yaml:"when"`
	Template string       `yaml:"template,omitempty"`
}

func (g GrafanaInstance) isEnabled() bool { return g.Enabled }

// SentryAuth holds authentication configuration for Sentry notifiers.
type SentryAuth struct {
	TokenFile string `yaml:"token_file"`
	TokenKey  string `yaml:"token_key,omitempty"`
}

// SentryInstance creates Sentry releases and deploy markers for successful
// runs (version = CommitSHA).
type SentryInstance struct {
	Name     string      `yaml:"name" validate:"required"`
	Enabled  bool        `yaml:"enabled"`
	BaseURL  string      `yaml:"base_url,omitempty"` // empty = sentry.io
	Org      string      `yaml:"org"`
	Projects []string    `yaml:"projects,omitempty"`
	Auth     *SentryAuth `yaml:"auth,omitempty"`
	When     string      `yaml:"when"`
}

func (s SentryInstance) isEnabled() bool { return s.Enabled }

// LoggingConfig contains logging configuration.
type LoggingConfig struct {
	Level   string        `yaml:"level"`   // debug, info, warn, error
	Verbose VerboseConfig `yaml:"verbose"` // verbose options (only apply when level=debug)
}

// VerboseConfig contains verbose logging options that only work when level is debug.
type VerboseConfig struct {
	Caller    bool `yaml:"caller"`     // Show file:line in logs
	HTTPCalls bool `yaml:"http_calls"` // Log HTTP request/response details
	Payloads  bool `yaml:"payloads"`   // Log request/response payloads
}

// TracingConfig contains OpenTelemetry tracing configuration.
type TracingConfig struct {
	Endpoint    string `mapstructure:"endpoint" yaml:"endpoint"`
	ServiceName string `mapstructure:"service_name" yaml:"service_name"`
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
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	applyDefaults(&cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return &cfg, nil
}

var envRe = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

// expandEnv handles ${UPPERCASE_VAR} for non-secret operational config.
// For secrets, use file-based resolution via secrets.Resolve.
func expandEnv(s string) string {
	return envRe.ReplaceAllStringFunc(s, func(match string) string {
		return os.Getenv(match[2 : len(match)-1])
	})
}

func applyDefaults(c *Config) {
	if c.Server.Addr == "" {
		c.Server.Addr = DefaultServerAddr
	}
	if c.Server.ReadTimeoutSec == 0 {
		c.Server.ReadTimeoutSec = 10
	}
	if c.Server.WriteTimeoutSec == 0 {
		c.Server.WriteTimeoutSec = 10
	}
	if c.Server.ShutdownTimeoutSec == 0 {
		c.Server.ShutdownTimeoutSec = 30
	}
	if c.DedupeSize == 0 {
		c.DedupeSize = 10000
	}
	if c.MaxConcurrency == 0 {
		c.MaxConcurrency = 100
	}
	if c.HandlerTimeout == 0 {
		c.HandlerTimeout = 10 * time.Second
	}
	if c.Retry.MaxAttempts == 0 {
		c.Retry.MaxAttempts = 4
	}
	if c.Retry.InitialBackoff == 0 {
		c.Retry.InitialBackoff = 250 * time.Millisecond
	}
	if c.Retry.MaxBackoff == 0 {
		c.Retry.MaxBackoff = 30 * time.Second
	}
	if c.Store.Backend == "" {
		c.Store.Backend = "memory"
	}
	if c.Store.TTL == 0 {
		c.Store.TTL = time.Hour
	}
	if c.DLQ.Path == "" {
		c.DLQ.Path = "/var/lib/tekton-events-relay/dlq.jsonl"
	}
	if c.DLQ.MaxSizeBytes == 0 {
		c.DLQ.MaxSizeBytes = 10 * 1024 * 1024
	}
	if !c.Filter.AllowTaskRun && !c.Filter.AllowPipelineRun {
		c.Filter.AllowPipelineRun = true
		c.Filter.IgnoreUnknown = true
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Tracing.Endpoint != "" && c.Tracing.ServiceName == "" {
		c.Tracing.ServiceName = "tekton-events-relay"
	}
}
