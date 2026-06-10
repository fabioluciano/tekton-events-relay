package config

import (
	"fmt"
	"net/url"
	"text/template"

	"github.com/itchyny/gojq"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
)

// Common validation rules

func requireBaseURL(prefix string, inst interface{}) []ValidationError {
	type hasEnabled interface {
		isEnabled() bool
	}
	if h, ok := inst.(hasEnabled); !ok || !h.isEnabled() {
		return nil
	}

	switch v := inst.(type) {
	case GitHubInstance:
		if v.BaseURL == "" {
			return []ValidationError{{Path: prefix + ".base_url", Message: ValidationMsgBaseURLRequired}}
		}
		if _, err := url.ParseRequestURI(v.BaseURL); err != nil {
			return []ValidationError{{Path: prefix + ".base_url", Message: fmt.Sprintf("invalid URL: %v", err)}}
		}
	case GitLabInstance:
		if v.BaseURL == "" {
			return []ValidationError{{Path: prefix + ".base_url", Message: ValidationMsgBaseURLRequired}}
		}
		if _, err := url.ParseRequestURI(v.BaseURL); err != nil {
			return []ValidationError{{Path: prefix + ".base_url", Message: fmt.Sprintf("invalid URL: %v", err)}}
		}
	case GiteaInstance:
		if v.BaseURL == "" {
			return []ValidationError{{Path: prefix + ".base_url", Message: ValidationMsgBaseURLRequired}}
		}
		if _, err := url.ParseRequestURI(v.BaseURL); err != nil {
			return []ValidationError{{Path: prefix + ".base_url", Message: fmt.Sprintf("invalid URL: %v", err)}}
		}
	case BitbucketInstance:
		if v.BaseURL == "" {
			return []ValidationError{{Path: prefix + ".base_url", Message: ValidationMsgBaseURLRequired}}
		}
		if _, err := url.ParseRequestURI(v.BaseURL); err != nil {
			return []ValidationError{{Path: prefix + ".base_url", Message: fmt.Sprintf("invalid URL: %v", err)}}
		}
	case AzureInstance:
		if v.BaseURL == "" {
			return []ValidationError{{Path: prefix + ".base_url", Message: ValidationMsgBaseURLRequired}}
		}
		if _, err := url.ParseRequestURI(v.BaseURL); err != nil {
			return []ValidationError{{Path: prefix + ".base_url", Message: fmt.Sprintf("invalid URL: %v", err)}}
		}
	case SourceHutInstance:
		if v.BaseURL == "" {
			return []ValidationError{{Path: prefix + ".base_url", Message: ValidationMsgBaseURLRequired}}
		}
		if _, err := url.ParseRequestURI(v.BaseURL); err != nil {
			return []ValidationError{{Path: prefix + ".base_url", Message: fmt.Sprintf("invalid URL: %v", err)}}
		}
	}
	return nil
}

func validateActions(prefix string, inst interface{}) []ValidationError {
	var actions []Action
	switch v := inst.(type) {
	case GitHubInstance:
		actions = v.Actions
	case GitLabInstance:
		actions = v.Actions
	case GiteaInstance:
		actions = v.Actions
	case BitbucketInstance:
		actions = v.Actions
	case AzureInstance:
		actions = v.Actions
	case SourceHutInstance:
		actions = v.Actions
	default:
		return nil
	}

	var errs []ValidationError
	for j, action := range actions {
		actionPrefix := fmt.Sprintf("%s.actions[%d]", prefix, j)
		errs = append(errs, validateAction(actionPrefix, action)...)
	}
	return errs
}

func validateAction(prefix string, action Action) []ValidationError {
	var errs []ValidationError

	// Name required validation is handled by validate:"required" struct tag.
	// Only validate enum values for non-empty type (empty type caught by struct tag).
	if action.Type != "" {
		// Validate action type against known types
		validTypes := map[ActionType]bool{
			ActionTypeCommitStatus:      true,
			ActionTypePRComment:         true,
			ActionTypeIssueComment:      true,
			ActionTypeLabel:             true,
			ActionTypeDiscussionComment: true,
			ActionTypeCheckRun:          true,
			ActionTypeDeploymentStatus:  true,
		}
		if !validTypes[action.Type] {
			errs = append(errs, ValidationError{
				Path:    prefix + ".type",
				Message: fmt.Sprintf("invalid action type '%s' (must be one of: commit_status, pr_comment, issue_comment, label, discussion_comment, check_run, deployment_status)", action.Type),
			})
		}
	}

	if action.When != "" {
		if _, err := cel.Compile(action.When); err != nil {
			errs = append(errs, ValidationError{
				Path:    prefix + ".when",
				Message: fmt.Sprintf("invalid CEL: %v", err),
			})
		}
	}

	if action.Template != "" {
		if _, err := template.New("test").Parse(action.Template); err != nil {
			errs = append(errs, ValidationError{
				Path:    prefix + ".template",
				Message: fmt.Sprintf("invalid template: %v", err),
			})
		}
	}

	return errs
}

func validateCELWhen(prefix string, inst interface{}) []ValidationError {
	var when string
	switch v := inst.(type) {
	case SlackInstance:
		when = v.When
	case TeamsInstance:
		when = v.When
	case DiscordInstance:
		when = v.When
	case PagerDutyInstance:
		when = v.When
	case DatadogInstance:
		when = v.When
	case WebhookInstance:
		when = v.When
	}

	if when != "" {
		if _, err := cel.Compile(when); err != nil {
			return []ValidationError{{
				Path:    prefix + ".when",
				Message: fmt.Sprintf("invalid CEL: %v", err),
			}}
		}
	}
	return nil
}

func validateTemplate(prefix string, inst interface{}) []ValidationError {
	var tmpl string
	switch v := inst.(type) {
	case SlackInstance:
		tmpl = v.Template
	case TeamsInstance:
		tmpl = v.Template
	case DiscordInstance:
		tmpl = v.Template
	}

	if tmpl != "" {
		if _, err := template.New("test").Parse(tmpl); err != nil {
			return []ValidationError{{
				Path:    prefix + ".template",
				Message: fmt.Sprintf("invalid template: %v", err),
			}}
		}
	}
	return nil
}

func validateWebhookAuth(prefix string, inst interface{}) []ValidationError {
	type hasEnabled interface {
		isEnabled() bool
	}
	if h, ok := inst.(hasEnabled); !ok || !h.isEnabled() {
		return nil
	}

	var auth *WebhookAuthConfig
	if v, ok := inst.(WebhookInstance); ok {
		auth = v.Auth
	}

	if auth == nil || auth.Type == "" {
		return nil
	}

	// Validate based on auth type
	switch auth.Type {
	case "bearer":
		if auth.TokenFile == "" {
			return []ValidationError{{Path: prefix + ".auth", Message: "type 'bearer' requires 'token_file'"}}
		}
		// Check for invalid fields
		if auth.UsernameFile != "" || auth.PasswordFile != "" || auth.SecretFile != "" || auth.Header != "" {
			return []ValidationError{{Path: prefix + ".auth", Message: "type 'bearer' does not accept 'username_file', 'password_file', 'secret_file', or 'header'"}}
		}
	case "basic":
		if auth.UsernameFile == "" || auth.PasswordFile == "" {
			return []ValidationError{{Path: prefix + ".auth", Message: "type 'basic' requires 'username_file' and 'password_file'"}}
		}
		// Check for invalid fields
		if auth.TokenFile != "" || auth.SecretFile != "" || auth.Header != "" {
			return []ValidationError{{Path: prefix + ".auth", Message: "type 'basic' does not accept 'token_file', 'secret_file', or 'header'"}}
		}
	case "apikey":
		if auth.TokenFile == "" || auth.Header == "" {
			return []ValidationError{{Path: prefix + ".auth", Message: "type 'apikey' requires 'token_file' and 'header'"}}
		}
		// Check for invalid fields
		if auth.UsernameFile != "" || auth.PasswordFile != "" || auth.SecretFile != "" {
			return []ValidationError{{Path: prefix + ".auth", Message: "type 'apikey' does not accept 'username_file', 'password_file', or 'secret_file'"}}
		}
	case "hmac":
		if auth.SecretFile == "" {
			return []ValidationError{{Path: prefix + ".auth", Message: "type 'hmac' requires 'secret_file'"}}
		}
		// Check for invalid fields
		if auth.TokenFile != "" || auth.UsernameFile != "" || auth.PasswordFile != "" || auth.Header != "" {
			return []ValidationError{{Path: prefix + ".auth", Message: "type 'hmac' does not accept 'token_file', 'username_file', 'password_file', or 'header'"}}
		}
	default:
		return []ValidationError{{Path: prefix + ".auth.type", Message: fmt.Sprintf("invalid type '%s'", auth.Type)}}
	}
	return nil
}

func validateTransform(prefix string, inst interface{}) []ValidationError {
	type hasEnabled interface {
		isEnabled() bool
	}
	if h, ok := inst.(hasEnabled); !ok || !h.isEnabled() {
		return nil
	}

	var transform string
	if v, ok := inst.(WebhookInstance); ok {
		transform = v.Transform
	}

	if transform != "" {
		if _, err := gojq.Parse(transform); err != nil {
			return []ValidationError{{
				Path:    prefix + ".transform",
				Message: "invalid jq syntax",
			}}
		}
	}
	return nil
}

// SCM provider validators

func validateGitHubInstance(prefix string, inst GitHubInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		var hasSecretFile, hasApp bool
		if inst.Auth != nil {
			hasSecretFile = inst.Auth.SecretFile != ""
			hasApp = inst.Auth.AppID != 0 && inst.Auth.InstallationID != 0
		}

		if hasSecretFile && hasApp {
			errs = append(errs, ValidationError{
				Path:    prefix + ".auth",
				Message: "cannot use both token (secret_file) and GitHub App auth (app_id, installation_id); choose one",
			})
		} else if !hasSecretFile && !hasApp {
			errs = append(errs, ValidationError{
				Path:    prefix,
				Message: "either 'auth.secret_file' or GitHub App credentials (auth.app_id, auth.installation_id) required when enabled; private key must be mounted at /etc/github-app/private-key.pem",
			})
		}

		var appFieldsSet bool
		if inst.Auth != nil {
			appFieldsSet = (inst.Auth.AppID != 0) || (inst.Auth.InstallationID != 0)
		}
		if appFieldsSet && !hasApp {
			errs = append(errs, ValidationError{
				Path:    prefix,
				Message: "when using GitHub App auth, both fields (auth.app_id, auth.installation_id) are required",
			})
		}
	}

	errs = append(errs, requireBaseURL(prefix, inst)...)
	errs = append(errs, validateActions(prefix, inst)...)

	return errs
}

func validateGitLabInstance(prefix string, inst GitLabInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		// Validate variant
		if inst.Variant != GitLabVariantSaaS && inst.Variant != GitLabVariantSelfManaged {
			errs = append(errs, ValidationError{
				Path:    prefix + ".variant",
				Message: fmt.Sprintf("variant must be 'saas' or 'self-managed', got '%s'", inst.Variant),
			})
		}

		// Auth validation
		if inst.Auth == nil {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		} else {
			hasSecret := inst.Auth.SecretFile != ""
			hasOAuth2 := inst.Auth.OAuth2 != nil
			if !hasSecret && !hasOAuth2 {
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "either auth.secret_file or auth.oauth2 required when enabled"})
			}
			if hasSecret && hasOAuth2 {
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "cannot use both auth.secret_file and auth.oauth2; choose one"})
			}
		}

		// base_url is required only for self-managed variant
		if inst.Variant == GitLabVariantSelfManaged && inst.BaseURL == "" {
			errs = append(errs, ValidationError{
				Path:    prefix + ".base_url",
				Message: "base_url is required for self-managed variant",
			})
		}
	}

	errs = append(errs, validateActions(prefix, inst)...)

	return errs
}

func validateGiteaInstance(prefix string, inst GiteaInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Auth == nil {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		} else {
			hasSecret := inst.Auth.SecretFile != ""
			hasOAuth2 := inst.Auth.OAuth2 != nil
			if !hasSecret && !hasOAuth2 {
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "either auth.secret_file or auth.oauth2 required when enabled"})
			}
			if hasSecret && hasOAuth2 {
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "cannot use both auth.secret_file and auth.oauth2; choose one"})
			}
		}
	}

	errs = append(errs, requireBaseURL(prefix, inst)...)
	errs = append(errs, validateActions(prefix, inst)...)

	return errs
}

func validateBitbucketInstance(prefix string, inst BitbucketInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Variant != BitbucketVariantCloud && inst.Variant != BitbucketVariantServer {
			errs = append(errs, ValidationError{
				Path:    prefix + ".variant",
				Message: fmt.Sprintf("variant must be 'cloud' or 'server', got '%s'", inst.Variant),
			})
		}

		if inst.Variant == BitbucketVariantCloud {
			if inst.Auth == nil {
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "variant 'cloud' requires auth"})
			} else {
				hasBasic := inst.Auth.UsernameFile != "" && inst.Auth.AppPasswordFile != ""
				hasOAuth2 := inst.Auth.OAuth2 != nil
				if !hasBasic && !hasOAuth2 {
					errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "variant 'cloud' requires either (username_file + app_password_file) or oauth2"})
				}
			}
		} else {
			if inst.Auth == nil {
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "variant 'server' requires auth"})
			} else if inst.Auth.TokenFile == "" {
				errs = append(errs, ValidationError{Path: prefix + ".auth.token_file", Message: "variant 'server' requires auth.token_file"})
			}
			// Only require base_url for server variant
			errs = append(errs, requireBaseURL(prefix, inst)...)
		}
	}

	errs = append(errs, validateActions(prefix, inst)...)

	return errs
}

func validateAzureInstance(prefix string, inst AzureInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled && inst.SecretFile == "" {
		errs = append(errs, ValidationError{Path: prefix + ".secret_file", Message: ValidationMsgRequiredForEnabled})
	}

	errs = append(errs, requireBaseURL(prefix, inst)...)
	errs = append(errs, validateActions(prefix, inst)...)

	return errs
}

func validateSourceHutInstance(prefix string, inst SourceHutInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Auth == nil || inst.Auth.SecretFile == "" {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		}
	}

	errs = append(errs, requireBaseURL(prefix, inst)...)
	errs = append(errs, validateActions(prefix, inst)...)

	return errs
}

// Notifier validators

// validateBotTokenAuth validates bot token authentication fields.
func validateBotTokenAuth(prefix string, botToken *BotTokenAuth) []ValidationError {
	var errs []ValidationError
	if botToken.TokenFile == "" {
		errs = append(errs, ValidationError{Path: prefix + ".token_file", Message: ValidationMsgRequired})
	}
	if botToken.ChannelID == "" {
		errs = append(errs, ValidationError{Path: prefix + ".channel_id", Message: ValidationMsgRequired})
	}
	return errs
}

func validateSlackInstance(prefix string, inst SlackInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Auth == nil {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		} else {
			hasWebhook := inst.Auth.WebhookURLFile != ""
			hasBot := inst.Auth.BotToken != nil
			if !hasWebhook && !hasBot {
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "either auth.webhook_url_file or auth.bot_token required when enabled"})
			}
			// Validate bot token nested fields
			if hasBot && inst.Auth.BotToken != nil {
				errs = append(errs, validateBotTokenAuth(prefix+".auth.bot_token", inst.Auth.BotToken)...)
			}
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)
	errs = append(errs, validateTemplate(prefix, inst)...)

	return errs
}

func validateTeamsInstance(prefix string, inst TeamsInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Auth == nil || inst.Auth.WebhookURLFile == "" {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)
	errs = append(errs, validateTemplate(prefix, inst)...)

	return errs
}

func validateDiscordInstance(prefix string, inst DiscordInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Auth == nil {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		} else {
			hasWebhook := inst.Auth.WebhookURLFile != ""
			hasBot := inst.Auth.BotToken != nil
			if !hasWebhook && !hasBot {
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "either auth.webhook_url_file or auth.bot_token required when enabled"})
			}
			// Validate bot token nested fields
			if hasBot && inst.Auth.BotToken != nil {
				errs = append(errs, validateBotTokenAuth(prefix+".auth.bot_token", inst.Auth.BotToken)...)
			}
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)
	errs = append(errs, validateTemplate(prefix, inst)...)

	return errs
}

func validatePagerDutyInstance(prefix string, inst PagerDutyInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Auth == nil || inst.Auth.IntegrationKeyFile == "" {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)

	return errs
}

func validateDatadogInstance(prefix string, inst DatadogInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Auth == nil || inst.Auth.APIKeyFile == "" {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)

	return errs
}

func validateWebhookInstance(prefix string, inst WebhookInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled && inst.URLFile == "" {
		errs = append(errs, ValidationError{Path: prefix + ".url_file", Message: "enabled instance missing required field 'url_file'"})
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)
	errs = append(errs, validateTransform(prefix, inst)...)
	errs = append(errs, validateWebhookAuth(prefix, inst)...)

	return errs
}

// Orchestration methods for validator.go

//nolint:dupl // validateSCM and validateNotifiers share structure but operate on different config sections
func (c *Config) validateSCM(names map[string]map[string]bool) error {
	for i, inst := range c.SCM.GitHub {
		if err := checkDuplicateName("scm.github", inst.Name, names); err != nil {
			return err
		}
		errs := validateGitHubInstance(fmt.Sprintf("scm.github[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	for i, inst := range c.SCM.GitLab {
		if err := checkDuplicateName("scm.gitlab", inst.Name, names); err != nil {
			return err
		}
		errs := validateGitLabInstance(fmt.Sprintf("scm.gitlab[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	for i, inst := range c.SCM.Gitea {
		if err := checkDuplicateName("scm.gitea", inst.Name, names); err != nil {
			return err
		}
		errs := validateGiteaInstance(fmt.Sprintf("scm.gitea[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	for i, inst := range c.SCM.Bitbucket {
		if err := checkDuplicateName("scm.bitbucket", inst.Name, names); err != nil {
			return err
		}
		errs := validateBitbucketInstance(fmt.Sprintf("scm.bitbucket[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	for i, inst := range c.SCM.Azure {
		if err := checkDuplicateName("scm.azure_devops", inst.Name, names); err != nil {
			return err
		}
		errs := validateAzureInstance(fmt.Sprintf("scm.azure_devops[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	for i, inst := range c.SCM.SourceHut {
		if err := checkDuplicateName("scm.sourcehut", inst.Name, names); err != nil {
			return err
		}
		errs := validateSourceHutInstance(fmt.Sprintf("scm.sourcehut[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	return nil
}

//nolint:dupl // validateSCM and validateNotifiers share structure but operate on different config sections
func (c *Config) validateNotifiers(names map[string]map[string]bool) error {
	for i, inst := range c.Notifiers.Slack {
		if err := checkDuplicateName("notifiers.slack", inst.Name, names); err != nil {
			return err
		}
		errs := validateSlackInstance(fmt.Sprintf("notifiers.slack[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	for i, inst := range c.Notifiers.Teams {
		if err := checkDuplicateName("notifiers.teams", inst.Name, names); err != nil {
			return err
		}
		errs := validateTeamsInstance(fmt.Sprintf("notifiers.teams[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	for i, inst := range c.Notifiers.Discord {
		if err := checkDuplicateName("notifiers.discord", inst.Name, names); err != nil {
			return err
		}
		errs := validateDiscordInstance(fmt.Sprintf("notifiers.discord[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	for i, inst := range c.Notifiers.PagerDuty {
		if err := checkDuplicateName("notifiers.pagerduty", inst.Name, names); err != nil {
			return err
		}
		errs := validatePagerDutyInstance(fmt.Sprintf("notifiers.pagerduty[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	for i, inst := range c.Notifiers.Datadog {
		if err := checkDuplicateName("notifiers.datadog", inst.Name, names); err != nil {
			return err
		}
		errs := validateDatadogInstance(fmt.Sprintf("notifiers.datadog[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	for i, inst := range c.Notifiers.Webhook {
		if err := checkDuplicateName("notifiers.webhook", inst.Name, names); err != nil {
			return err
		}
		errs := validateWebhookInstance(fmt.Sprintf("notifiers.webhook[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}

	return nil
}

// Helper for duplicate name checks
func checkDuplicateName(providerPath string, name string, names map[string]map[string]bool) error {
	if names[providerPath] == nil {
		names[providerPath] = make(map[string]bool)
	}
	if names[providerPath][name] {
		return fmt.Errorf("%s: duplicate instance name '%s'", providerPath, name)
	}
	names[providerPath][name] = true
	return nil
}
