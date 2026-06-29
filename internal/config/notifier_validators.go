// Package config provides configuration loading, validation, and hot-reload
// for the tekton-events-relay binary.
package config

import (
	"fmt"
	"text/template"
)

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

//nolint:dupl // validateSlackInstance and validateDiscordInstance share structure but operate on different config types
func validateSlackInstance(prefix string, inst SlackInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Auth == nil {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		} else {
			hasWebhook := inst.Auth.WebhookURLFile != ""
			hasBot := inst.Auth.BotToken != nil
			switch {
			case !hasWebhook && !hasBot:
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "exactly one of auth.webhook_url_file or auth.bot_token required when enabled"})
			case hasWebhook && hasBot:
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "auth.webhook_url_file and auth.bot_token are mutually exclusive"})
			}
			// Validate bot token nested fields
			if hasBot && inst.Auth.BotToken != nil {
				errs = append(errs, validateBotTokenAuth(prefix+".auth.bot_token", inst.Auth.BotToken)...)
			}
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)
	errs = append(errs, validateTemplate(prefix, inst)...)

	if inst.ThreadMode != "" && inst.ThreadMode != "grouped" {
		errs = append(errs, ValidationError{Path: prefix + ".thread_mode", Message: "thread_mode must be empty or \"grouped\""})
	}
	if inst.ThreadMode == "grouped" && inst.Mode == "upsert" {
		errs = append(errs, ValidationError{Path: prefix + ".thread_mode", Message: "thread_mode \"grouped\" and mode \"upsert\" are mutually exclusive"})
	}
	if inst.ThreadMode == "grouped" && inst.Auth != nil && inst.Auth.BotToken == nil {
		errs = append(errs, ValidationError{Path: prefix + ".thread_mode", Message: "thread_mode \"grouped\" requires bot_token auth"})
	}

	if inst.ChannelExpr != "" {
		if err := validateCELStringExpression(inst.ChannelExpr); err != nil {
			errs = append(errs, ValidationError{Path: prefix + ".channel_expr", Message: fmt.Sprintf("invalid CEL: %v", err)})
		}
	}

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

	for i, mu := range inst.MentionUsers {
		if mu.ID == "" {
			errs = append(errs, ValidationError{
				Path:    fmt.Sprintf("%s.mention_users[%d].id", prefix, i),
				Message: "id is required for mention_users entries",
			})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)
	errs = append(errs, validateTemplate(prefix, inst)...)

	return errs
}

//nolint:dupl // validateDiscordInstance and validateSlackInstance share structure but operate on different config types
func validateDiscordInstance(prefix string, inst DiscordInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Auth == nil {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		} else {
			hasWebhook := inst.Auth.WebhookURLFile != ""
			hasBot := inst.Auth.BotToken != nil
			switch {
			case !hasWebhook && !hasBot:
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "exactly one of auth.webhook_url_file or auth.bot_token required when enabled"})
			case hasWebhook && hasBot:
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "auth.webhook_url_file and auth.bot_token are mutually exclusive"})
			}
			// Validate bot token nested fields
			if hasBot && inst.Auth.BotToken != nil {
				errs = append(errs, validateBotTokenAuth(prefix+".auth.bot_token", inst.Auth.BotToken)...)
			}
		}
	}

	for i, id := range inst.MentionRoles {
		if id == "" {
			errs = append(errs, ValidationError{
				Path:    fmt.Sprintf("%s.mention_roles[%d]", prefix, i),
				Message: "role ID must not be empty",
			})
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

func validateOpsgenieInstance(prefix string, inst OpsgenieInstance) []ValidationError {
	var errs []ValidationError

	if inst.Enabled {
		if inst.Auth == nil || inst.Auth.APIKeyFile == "" {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		}
	}

	if inst.Priority != "" {
		switch inst.Priority {
		case "P1", "P2", "P3", "P4", "P5":
		default:
			errs = append(errs, ValidationError{Path: prefix + ".priority", Message: "priority must be one of P1, P2, P3, P4, P5"})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)

	return errs
}

func validateNewRelicInstance(prefix string, inst NewRelicInstance) []ValidationError {
	var errs []ValidationError

	if inst.Enabled {
		if inst.Auth == nil || inst.Auth.APIKeyFile == "" {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		}
		if inst.AccountID == "" {
			errs = append(errs, ValidationError{Path: prefix + ".account_id", Message: ValidationMsgRequired})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)

	return errs
}

func validateHoneycombInstance(prefix string, inst HoneycombInstance) []ValidationError {
	var errs []ValidationError

	if inst.Enabled {
		if inst.Auth == nil || inst.Auth.APIKeyFile == "" {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		}
		if inst.Dataset == "" {
			errs = append(errs, ValidationError{Path: prefix + ".dataset", Message: ValidationMsgRequired})
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

func validateGrafanaInstance(prefix string, inst GrafanaInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.URL == "" {
			errs = append(errs, ValidationError{Path: prefix + ".url", Message: ValidationMsgBaseURLRequired})
		}
		if inst.Auth == nil || inst.Auth.TokenFile == "" {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		}
		// Category-1: template is required (no native fallback).
		if inst.Template == "" {
			errs = append(errs, ValidationError{Path: prefix + ".template", Message: ValidationMsgRequiredForEnabled})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)
	errs = append(errs, validateTemplate(prefix, inst)...)

	return errs
}

func validateSentryInstance(prefix string, inst SentryInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Auth == nil || inst.Auth.TokenFile == "" {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		}
		if inst.Org == "" {
			errs = append(errs, ValidationError{Path: prefix + ".org", Message: ValidationMsgRequired})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)

	return errs
}

func validateIncidentIOInstance(prefix string, inst IncidentIOInstance) []ValidationError {
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

func validateTelegramInstance(prefix string, inst TelegramInstance) []ValidationError {
	var errs []ValidationError

	if inst.Enabled {
		if inst.Auth == nil || inst.Auth.TokenFile == "" {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		}
		if inst.ChatID == "" {
			errs = append(errs, ValidationError{Path: prefix + ".chat_id", Message: ValidationMsgRequired})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)
	errs = append(errs, validateTemplate(prefix, inst)...)

	return errs
}

func validateMattermostInstance(prefix string, inst MattermostInstance) []ValidationError {
	var errs []ValidationError
	// Name validation handled by validate:"required" struct tag

	if inst.Enabled {
		if inst.Auth == nil {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		} else {
			hasWebhook := inst.Auth.WebhookURLFile != ""
			hasBot := inst.Auth.BotToken != nil
			switch {
			case !hasWebhook && !hasBot:
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "exactly one of auth.webhook_url_file or auth.bot_token required when enabled"})
			case hasWebhook && hasBot:
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "auth.webhook_url_file and auth.bot_token are mutually exclusive"})
			}
			// Validate bot token nested fields
			if hasBot && inst.Auth.BotToken != nil {
				errs = append(errs, validateBotTokenAuth(prefix+".auth.bot_token", inst.Auth.BotToken)...)
			}
			// Bot token mode requires base_url
			if hasBot && inst.BaseURL == "" {
				errs = append(errs, ValidationError{Path: prefix + ".base_url", Message: "base_url required for bot token mode"})
			}
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)
	errs = append(errs, validateTemplate(prefix, inst)...)

	return errs
}

func validateEmailInstance(prefix string, inst EmailInstance) []ValidationError {
	var errs []ValidationError
	if inst.Enabled {
		if inst.Host == "" {
			errs = append(errs, ValidationError{Path: prefix + ".host", Message: "host required when enabled"})
		}
		if inst.From == "" {
			errs = append(errs, ValidationError{Path: prefix + ".from", Message: "from required when enabled"})
		}
		if len(inst.To) == 0 {
			errs = append(errs, ValidationError{Path: prefix + ".to", Message: "at least one recipient required when enabled"})
		}
		if inst.Port < 0 || inst.Port > 65535 {
			errs = append(errs, ValidationError{Path: prefix + ".port", Message: "port must be between 1 and 65535"})
		}
		// Category-1: subject and body templates are required (no native fallback).
		if inst.Subject == "" {
			errs = append(errs, ValidationError{Path: prefix + ".subject", Message: ValidationMsgRequiredForEnabled})
		} else if _, err := template.New("subject").Parse(inst.Subject); err != nil {
			errs = append(errs, ValidationError{Path: prefix + ".subject", Message: fmt.Sprintf("invalid template: %v", err)})
		}
		if inst.Template == "" {
			errs = append(errs, ValidationError{Path: prefix + ".template", Message: ValidationMsgRequiredForEnabled})
		}
		if inst.Auth != nil && inst.Auth.XOAuth2 {
			if inst.Auth.Username == "" {
				errs = append(errs, ValidationError{Path: prefix + ".auth.username", Message: "username required for xoauth2"})
			}
			if inst.Auth.TokenFile == "" {
				errs = append(errs, ValidationError{Path: prefix + ".auth.token_file", Message: "token_file required for xoauth2"})
			}
			if inst.Auth.PasswordFile != "" {
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "cannot use both auth.xoauth2 and auth.password_file; choose one"})
			}
		} else if inst.Auth != nil {
			if inst.Auth.Username != "" && inst.Auth.PasswordFile == "" {
				errs = append(errs, ValidationError{Path: prefix + ".auth.password_file", Message: "password_file required when auth.username is set"})
			}
			if inst.Auth.Username == "" && inst.Auth.PasswordFile != "" {
				errs = append(errs, ValidationError{Path: prefix + ".auth.username", Message: "username required when auth.password_file is set"})
			}
		}
	}
	errs = append(errs, validateCELWhen(prefix, inst)...)
	errs = append(errs, validateTemplate(prefix, inst)...)

	return errs
}

//nolint:gocyclo // Jira validation has many mutually exclusive auth paths (Cloud basic, DC bearer, OAuth2)
func validateJiraInstance(prefix string, inst JiraInstance) []ValidationError {
	var errs []ValidationError
	if inst.Enabled {
		if inst.BaseURL == "" {
			errs = append(errs, ValidationError{Path: prefix + ".base_url", Message: "base_url required when enabled"})
		}
		if inst.APIVersion != "" && inst.APIVersion != "2" && inst.APIVersion != "3" {
			errs = append(errs, ValidationError{Path: prefix + ".api_version", Message: fmt.Sprintf("invalid api_version '%s' (must be 2 or 3)", inst.APIVersion)})
		}
		if inst.Auth == nil {
			errs = append(errs, ValidationError{Path: prefix + ValidationPathAuth, Message: ValidationMsgAuthRequired})
		} else {
			if inst.Auth.Email != "" && inst.Auth.OAuth2 != nil {
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "cannot use both auth.email (basic auth) and auth.oauth2; choose one"})
			}
			if inst.Auth.TokenFile != "" && inst.Auth.OAuth2 != nil {
				errs = append(errs, ValidationError{Path: prefix + ".auth", Message: "cannot use both auth.token_file and auth.oauth2; choose one"})
			}
			if inst.Auth.OAuth2 == nil && inst.Auth.TokenFile == "" {
				errs = append(errs, ValidationError{Path: prefix + ".auth.token_file", Message: ValidationMsgRequiredForEnabled})
			}
			errs = append(errs, validateOAuth2(prefix+".auth", inst.Auth.OAuth2)...)
		}
	}
	for i, action := range inst.Actions {
		aprefix := fmt.Sprintf("%s.actions[%d]", prefix, i)
		switch action.Type {
		case JiraActionComment:
			// Category-1: comment template is required (no native fallback).
			if inst.Enabled && action.Enabled && action.Template == "" {
				errs = append(errs, ValidationError{Path: aprefix + ".template", Message: ValidationMsgRequiredForEnabled})
			}
			if action.Template != "" {
				if _, err := template.New("jira").Parse(action.Template); err != nil {
					errs = append(errs, ValidationError{Path: aprefix + ".template", Message: fmt.Sprintf("invalid template: %v", err)})
				}
			}
		case JiraActionTransition:
			if action.Transition == "" {
				errs = append(errs, ValidationError{Path: aprefix + ".transition", Message: "transition name or id required"})
			}
		case JiraActionCreateIssue:
			if inst.Enabled && action.Enabled && action.ProjectKey == "" {
				errs = append(errs, ValidationError{Path: aprefix + ".project_key", Message: ValidationMsgRequiredForEnabled})
			}
		case JiraActionLinkCommit:
			if inst.Enabled && action.Enabled && action.IssueKey == "" {
				errs = append(errs, ValidationError{Path: aprefix + ".issue_key", Message: ValidationMsgRequiredForEnabled})
			}
		default:
			errs = append(errs, ValidationError{Path: aprefix + ".type", Message: fmt.Sprintf("invalid jira action type '%s' (must be comment, transition, create_issue, or link_commit)", action.Type)})
		}
		if action.When != "" {
			if err := validateCELExpression(action.When); err != nil {
				errs = append(errs, ValidationError{Path: aprefix + ".when", Message: fmt.Sprintf("invalid CEL: %v", err)})
			}
		}
	}
	return errs
}

// validateJira validates all Jira instances.
func (c *Config) validateJira(names map[string]map[string]bool) error {
	for i, inst := range c.Jira {
		if err := checkDuplicateName("jira", inst.Name, names); err != nil {
			return err
		}
		errs := validateJiraInstance(fmt.Sprintf("jira[%d]", i), inst)
		if len(errs) > 0 {
			return fmt.Errorf("%s", errs[0].Error())
		}
	}
	return nil
}

//nolint:dupl,gocyclo // validateSCM and validateNotifiers share structure but operate on different config sections; high cyclomatic complexity from many notifier types
func (c *Config) validateNotifiers(names map[string]map[string]bool) error {
	for i, inst := range c.Notifiers.Slack {
		if err := checkDuplicateName("notifiers.slack", inst.Name, names); err != nil {
			return err
		}
		errs := validateSlackInstance(fmt.Sprintf("notifiers.slack[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.Teams {
		if err := checkDuplicateName("notifiers.teams", inst.Name, names); err != nil {
			return err
		}
		errs := validateTeamsInstance(fmt.Sprintf("notifiers.teams[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.Email {
		if err := checkDuplicateName("notifiers.email", inst.Name, names); err != nil {
			return err
		}
		errs := validateEmailInstance(fmt.Sprintf("notifiers.email[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.Discord {
		if err := checkDuplicateName("notifiers.discord", inst.Name, names); err != nil {
			return err
		}
		errs := validateDiscordInstance(fmt.Sprintf("notifiers.discord[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.PagerDuty {
		if err := checkDuplicateName("notifiers.pagerduty", inst.Name, names); err != nil {
			return err
		}
		errs := validatePagerDutyInstance(fmt.Sprintf("notifiers.pagerduty[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.Datadog {
		if err := checkDuplicateName("notifiers.datadog", inst.Name, names); err != nil {
			return err
		}
		errs := validateDatadogInstance(fmt.Sprintf("notifiers.datadog[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.Webhook {
		if err := checkDuplicateName("notifiers.webhook", inst.Name, names); err != nil {
			return err
		}
		errs := validateWebhookInstance(fmt.Sprintf("notifiers.webhook[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.Grafana {
		if err := checkDuplicateName("notifiers.grafana", inst.Name, names); err != nil {
			return err
		}
		errs := validateGrafanaInstance(fmt.Sprintf("notifiers.grafana[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.Sentry {
		if err := checkDuplicateName("notifiers.sentry", inst.Name, names); err != nil {
			return err
		}
		errs := validateSentryInstance(fmt.Sprintf("notifiers.sentry[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.Mattermost {
		if err := checkDuplicateName("notifiers.mattermost", inst.Name, names); err != nil {
			return err
		}
		errs := validateMattermostInstance(fmt.Sprintf("notifiers.mattermost[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.Opsgenie {
		if err := checkDuplicateName("notifiers.opsgenie", inst.Name, names); err != nil {
			return err
		}
		errs := validateOpsgenieInstance(fmt.Sprintf("notifiers.opsgenie[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.IncidentIO {
		if err := checkDuplicateName("notifiers.incidentio", inst.Name, names); err != nil {
			return err
		}
		errs := validateIncidentIOInstance(fmt.Sprintf("notifiers.incidentio[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.Notifiers.Honeycomb {
		if err := checkDuplicateName("notifiers.honeycomb", inst.Name, names); err != nil {
			return err
		}
		errs := validateHoneycombInstance(fmt.Sprintf("notifiers.honeycomb[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	return nil
}

// Webhook auth sub-validators

func validateWebhookAuth(prefix string, inst any) []ValidationError {
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

	// Validate based on auth type (split per type to keep complexity low).
	switch auth.Type {
	case "bearer":
		return validateWebhookBearerAuth(prefix, auth)
	case "basic":
		return validateWebhookBasicAuth(prefix, auth)
	case "apikey":
		return validateWebhookAPIKeyAuth(prefix, auth)
	case "hmac":
		return validateWebhookHMACAuth(prefix, auth)
	case "oauth2":
		return validateWebhookOAuth2Auth(prefix, auth)
	default:
		return []ValidationError{{Path: prefix + ".auth.type", Message: fmt.Sprintf("invalid type '%s'", auth.Type)}}
	}
}

func validateWebhookBearerAuth(prefix string, auth *WebhookAuthConfig) []ValidationError {
	if auth.TokenFile == "" {
		return []ValidationError{{Path: prefix + ".auth", Message: "type 'bearer' requires 'token_file'"}}
	}
	if auth.UsernameFile != "" || auth.PasswordFile != "" || auth.SecretFile != "" || auth.Header != "" {
		return []ValidationError{{Path: prefix + ".auth", Message: "type 'bearer' does not accept 'username_file', 'password_file', 'secret_file', or 'header'"}}
	}
	return nil
}

func validateWebhookBasicAuth(prefix string, auth *WebhookAuthConfig) []ValidationError {
	if auth.UsernameFile == "" || auth.PasswordFile == "" {
		return []ValidationError{{Path: prefix + ".auth", Message: "type 'basic' requires 'username_file' and 'password_file'"}}
	}
	if auth.TokenFile != "" || auth.SecretFile != "" || auth.Header != "" {
		return []ValidationError{{Path: prefix + ".auth", Message: "type 'basic' does not accept 'token_file', 'secret_file', or 'header'"}}
	}
	return nil
}

func validateWebhookAPIKeyAuth(prefix string, auth *WebhookAuthConfig) []ValidationError {
	if auth.TokenFile == "" || auth.Header == "" {
		return []ValidationError{{Path: prefix + ".auth", Message: "type 'apikey' requires 'token_file' and 'header'"}}
	}
	if auth.UsernameFile != "" || auth.PasswordFile != "" || auth.SecretFile != "" {
		return []ValidationError{{Path: prefix + ".auth", Message: "type 'apikey' does not accept 'username_file', 'password_file', or 'secret_file'"}}
	}
	return nil
}

func validateWebhookHMACAuth(prefix string, auth *WebhookAuthConfig) []ValidationError {
	if auth.SecretFile == "" {
		return []ValidationError{{Path: prefix + ".auth", Message: "type 'hmac' requires 'secret_file'"}}
	}
	if auth.TokenFile != "" || auth.UsernameFile != "" || auth.PasswordFile != "" || auth.Header != "" {
		return []ValidationError{{Path: prefix + ".auth", Message: "type 'hmac' does not accept 'token_file', 'username_file', 'password_file', or 'header'"}}
	}
	return nil
}

func validateWebhookOAuth2Auth(prefix string, auth *WebhookAuthConfig) []ValidationError {
	if auth.OAuth2 == nil {
		return []ValidationError{{Path: prefix + ".auth", Message: "type 'oauth2' requires an 'oauth2' block"}}
	}
	if auth.TokenFile != "" || auth.UsernameFile != "" || auth.PasswordFile != "" || auth.SecretFile != "" || auth.Header != "" {
		return []ValidationError{{Path: prefix + ".auth", Message: "type 'oauth2' does not accept 'token_file', 'username_file', 'password_file', 'secret_file', or 'header'"}}
	}
	return validateOAuth2(prefix+".auth", auth.OAuth2)
}

func validateNATSInstance(prefix string, inst NATSInstance) []ValidationError {
	var errs []ValidationError

	if inst.Enabled {
		if len(inst.Servers) == 0 {
			errs = append(errs, ValidationError{Path: prefix + ".servers", Message: ValidationMsgRequiredForEnabled})
		}
		if inst.Subject == "" {
			errs = append(errs, ValidationError{Path: prefix + ".subject", Message: ValidationMsgRequiredForEnabled})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)

	return errs
}

func validateRabbitMQInstance(prefix string, inst RabbitMQInstance) []ValidationError {
	var errs []ValidationError

	if inst.Enabled {
		if inst.URLFile == "" {
			errs = append(errs, ValidationError{Path: prefix + ".url_file", Message: ValidationMsgRequiredForEnabled})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)

	return errs
}

func validateRedisPubSubInstance(prefix string, inst RedisPubSubInstance) []ValidationError {
	var errs []ValidationError

	if inst.Enabled {
		if inst.Address == "" {
			errs = append(errs, ValidationError{Path: prefix + ".address", Message: ValidationMsgRequiredForEnabled})
		}
		if inst.Channel == "" {
			errs = append(errs, ValidationError{Path: prefix + ".channel", Message: ValidationMsgRequiredForEnabled})
		}
	}

	errs = append(errs, validateCELWhen(prefix, inst)...)

	return errs
}
