package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
)

// ValidationError represents a configuration validation error with path and message.
type ValidationError struct {
	Path    string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// configValidator is the package-level validator instance with custom registrations.
var configValidator *validator.Validate

func init() {
	configValidator = NewValidator()
}

// NewValidator creates a configured validator.Validate instance with all custom
// struct-level validations registered for config types.
func NewValidator() *validator.Validate {
	v := validator.New()

	// Use yaml tag name for field names in errors
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := fld.Tag.Get("yaml")
		if name == "" {
			return ""
		}
		// Strip ",omitempty" or other options
		if idx := strings.Index(name, ","); idx != -1 {
			name = name[:idx]
		}
		return name
	})

	// Register custom struct-level validators for conditional validation
	v.RegisterStructValidation(validateServerAuthStruct, AuthConfig{})

	return v
}

// ValidateAll performs comprehensive validation of the entire configuration.
// It uses go-playground/validator for struct tag validation (required fields)
// and custom struct-level validators for conditional/complex validation logic.
func ValidateAll(cfg *Config) []ValidationError {
	var errs []ValidationError

	// Run struct tag validation via go-playground/validator
	if err := configValidator.Struct(cfg); err != nil {
		var validationErrors validator.ValidationErrors
		if errors.As(err, &validationErrors) {
			for _, fe := range validationErrors {
				errs = append(errs, fieldErrorToValidationError(fe))
			}
		}
	}

	// Run custom per-instance validators that handle conditional logic,
	// CEL compilation, template parsing, and URL validation.
	// These produce errors with exact paths matching the existing format.
	for i, inst := range cfg.SCM.GitHub {
		prefix := fmt.Sprintf("scm.github[%d]", i)
		errs = append(errs, validateGitHubInstance(prefix, inst)...)
	}

	for i, inst := range cfg.SCM.GitLab {
		prefix := fmt.Sprintf("scm.gitlab[%d]", i)
		errs = append(errs, validateGitLabInstance(prefix, inst)...)
	}

	for i, inst := range cfg.SCM.Bitbucket {
		prefix := fmt.Sprintf("scm.bitbucket[%d]", i)
		errs = append(errs, validateBitbucketInstance(prefix, inst)...)
	}

	for i, inst := range cfg.SCM.Azure {
		prefix := fmt.Sprintf("scm.azure_devops[%d]", i)
		errs = append(errs, validateAzureInstance(prefix, inst)...)
	}

	for i, inst := range cfg.SCM.Gitea {
		prefix := fmt.Sprintf("scm.gitea[%d]", i)
		errs = append(errs, validateGiteaInstance(prefix, inst)...)
	}

	for i, inst := range cfg.SCM.SourceHut {
		prefix := fmt.Sprintf("scm.sourcehut[%d]", i)
		errs = append(errs, validateSourceHutInstance(prefix, inst)...)
	}

	for i, inst := range cfg.Notifiers.Slack {
		prefix := fmt.Sprintf("notifiers.slack[%d]", i)
		errs = append(errs, validateSlackInstance(prefix, inst)...)
	}

	for i, inst := range cfg.Notifiers.Teams {
		prefix := fmt.Sprintf("notifiers.teams[%d]", i)
		errs = append(errs, validateTeamsInstance(prefix, inst)...)
	}

	for i, inst := range cfg.Notifiers.Discord {
		prefix := fmt.Sprintf("notifiers.discord[%d]", i)
		errs = append(errs, validateDiscordInstance(prefix, inst)...)
	}

	for i, inst := range cfg.Notifiers.PagerDuty {
		prefix := fmt.Sprintf("notifiers.pagerduty[%d]", i)
		errs = append(errs, validatePagerDutyInstance(prefix, inst)...)
	}

	for i, inst := range cfg.Notifiers.Datadog {
		prefix := fmt.Sprintf("notifiers.datadog[%d]", i)
		errs = append(errs, validateDatadogInstance(prefix, inst)...)
	}

	for i, inst := range cfg.Notifiers.Webhook {
		prefix := fmt.Sprintf("notifiers.webhook[%d]", i)
		errs = append(errs, validateWebhookInstance(prefix, inst)...)
	}

	for i, inst := range cfg.Notifiers.Grafana {
		prefix := fmt.Sprintf("notifiers.grafana[%d]", i)
		errs = append(errs, validateGrafanaInstance(prefix, inst)...)
	}

	for i, inst := range cfg.Notifiers.Sentry {
		prefix := fmt.Sprintf("notifiers.sentry[%d]", i)
		errs = append(errs, validateSentryInstance(prefix, inst)...)
	}

	for i, inst := range cfg.Notifiers.Email {
		prefix := fmt.Sprintf("notifiers.email[%d]", i)
		errs = append(errs, validateEmailInstance(prefix, inst)...)
	}

	for i, inst := range cfg.Jira {
		prefix := fmt.Sprintf("jira[%d]", i)
		errs = append(errs, validateJiraInstance(prefix, inst)...)
	}

	return errs
}

// fieldErrorToValidationError converts a validator.FieldError to our ValidationError format,
// preserving the path format used by existing tests.
func fieldErrorToValidationError(fe validator.FieldError) ValidationError {
	path := translateFieldPath(fe.Namespace())
	msg := translateFieldMessage(fe)
	return ValidationError{Path: path, Message: msg}
}

// translateFieldPath converts validator namespace paths to our dot-notation format.
// e.g. "Config.server.addr" -> "server.addr"
// e.g. "Config.scm.github[0].name" -> "scm.github[0].name"
func translateFieldPath(namespace string) string {
	// Remove the root struct name (e.g. "Config.")
	if idx := strings.Index(namespace, "."); idx != -1 {
		namespace = namespace[idx+1:]
	}

	// The namespace already uses yaml tag names due to RegisterTagNameFunc
	return namespace
}

// translateFieldMessage converts validator tag to our message format.
func translateFieldMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return ValidationMsgRequired
	case "required_when_enabled":
		return ValidationMsgRequiredWhenEnabled
	case "unsupported_auth_type":
		return fmt.Sprintf("unsupported auth type '%s' (must be 'hmac-sha256' or 'bearer')", fe.Param())
	case "hmac_requires_timestamp":
		return ValidationMsgHMACReplayRequired
	default:
		return fmt.Sprintf("failed on '%s' validation", fe.Tag())
	}
}

// Struct-level validators registered with go-playground/validator.
// These handle conditional validation that cannot be expressed via struct tags.

func validateServerAuthStruct(sl validator.StructLevel) {
	auth := sl.Current().Interface().(AuthConfig)
	if auth.Enabled {
		switch auth.Type {
		case AuthTypeHMACSHA256, AuthTypeBearer:
		case "":
			sl.ReportError(auth.Type, "type", "type", "required_when_enabled", "")
		default:
			sl.ReportError(auth.Type, "type", "type", "unsupported_auth_type", auth.Type)
		}
		if auth.SecretFile == "" {
			sl.ReportError(auth.SecretFile, "secret_file", "secret_file", "required_when_enabled", "")
		}
		if auth.Type == AuthTypeHMACSHA256 && !auth.ValidateTimestamp {
			sl.ReportError(auth.ValidateTimestamp, "validate_timestamp", "validate_timestamp", "hmac_requires_timestamp", "")
		}
	}
}

// Validate performs runtime validation checks on the configuration.
func (c *Config) Validate() error {
	if err := c.validateLogging(); err != nil {
		return err
	}
	if err := c.validateServer(); err != nil {
		return err
	}
	if err := c.validateStore(); err != nil {
		return err
	}
	if err := c.validateRetry(); err != nil {
		return err
	}
	if err := c.validateLimits(); err != nil {
		return err
	}
	if err := c.validateTracing(); err != nil {
		return err
	}
	if err := c.validateTLS(); err != nil {
		return err
	}

	names := make(map[string]map[string]bool)
	if err := c.validateSCM(names); err != nil {
		return err
	}
	if err := c.validateNotifiers(names); err != nil {
		return err
	}
	if err := c.validateJira(names); err != nil {
		return err
	}
	return nil
}

// ValidateTokenReferences validates that all token references exist in the secrets store.
func (c *Config) ValidateTokenReferences(log *zap.Logger) {
	checkToken := func(name, token string) {
		if token == "" {
			return
		}
		if !strings.HasPrefix(token, "/") {
			log.Warn("token should be an absolute file path, not a literal value",
				zap.String("field", name),
				zap.String("hint", "use absolute path like /etc/secrets/provider/instance/key"))
		}
	}

	for _, inst := range c.SCM.GitHub {
		if inst.Auth != nil {
			checkToken("scm.github.auth.secret_file", inst.Auth.SecretFile)
		}
	}
	for _, inst := range c.SCM.GitLab {
		if inst.Auth != nil {
			checkToken("scm.gitlab.auth.secret_file", inst.Auth.SecretFile)
		}
	}
	for _, inst := range c.SCM.Bitbucket {
		if inst.Auth != nil {
			checkToken("scm.bitbucket.auth.username_file", inst.Auth.UsernameFile)
			checkToken("scm.bitbucket.auth.app_password_file", inst.Auth.AppPasswordFile)
			checkToken("scm.bitbucket.auth.token_file", inst.Auth.TokenFile)
		}
	}
	for _, inst := range c.SCM.Azure {
		checkToken("scm.azure.secret_file", inst.SecretFile)
	}
	for _, inst := range c.SCM.Gitea {
		if inst.Auth != nil {
			checkToken("scm.gitea.auth.secret_file", inst.Auth.SecretFile)
		}
	}
	for _, inst := range c.SCM.SourceHut {
		if inst.Auth != nil {
			checkToken("scm.sourcehut.auth.secret_file", inst.Auth.SecretFile)
		}
	}
	for _, inst := range c.Notifiers.Slack {
		if inst.Auth != nil {
			checkToken("notifiers.slack.auth.webhook_url_file", inst.Auth.WebhookURLFile)
			if inst.Auth.BotToken != nil {
				checkToken("notifiers.slack.auth.bot_token.token_file", inst.Auth.BotToken.TokenFile)
			}
		}
	}
	for _, inst := range c.Notifiers.Teams {
		if inst.Auth != nil {
			checkToken("notifiers.teams.auth.webhook_url_file", inst.Auth.WebhookURLFile)
		}
	}
	for _, inst := range c.Notifiers.Discord {
		if inst.Auth != nil {
			checkToken("notifiers.discord.auth.webhook_url_file", inst.Auth.WebhookURLFile)
			if inst.Auth.BotToken != nil {
				checkToken("notifiers.discord.auth.bot_token.token_file", inst.Auth.BotToken.TokenFile)
			}
		}
	}
	for _, inst := range c.Notifiers.Datadog {
		if inst.Auth != nil {
			checkToken("notifiers.datadog.auth.api_key_file", inst.Auth.APIKeyFile)
		}
	}
	for _, inst := range c.Notifiers.PagerDuty {
		if inst.Auth != nil {
			checkToken("notifiers.pagerduty.auth.integration_key_file", inst.Auth.IntegrationKeyFile)
		}
	}
	if c.Server.Auth.Enabled {
		checkToken("server.auth.secret_file", c.Server.Auth.SecretFile)
	}
}
