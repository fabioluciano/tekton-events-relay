// Package config provides configuration loading, validation, and hot-reload
// for the tekton-events-relay binary.
package config

import "fmt"

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

//nolint:dupl // validateSCM and validateNotifiers share structure but operate on different config sections
func (c *Config) validateSCM(names map[string]map[string]bool) error {
	for i, inst := range c.SCM.GitHub {
		if err := checkDuplicateName("scm.github", inst.Name, names); err != nil {
			return err
		}
		errs := validateGitHubInstance(fmt.Sprintf("scm.github[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.SCM.GitLab {
		if err := checkDuplicateName("scm.gitlab", inst.Name, names); err != nil {
			return err
		}
		errs := validateGitLabInstance(fmt.Sprintf("scm.gitlab[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.SCM.Gitea {
		if err := checkDuplicateName("scm.gitea", inst.Name, names); err != nil {
			return err
		}
		errs := validateGiteaInstance(fmt.Sprintf("scm.gitea[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.SCM.Bitbucket {
		if err := checkDuplicateName("scm.bitbucket", inst.Name, names); err != nil {
			return err
		}
		errs := validateBitbucketInstance(fmt.Sprintf("scm.bitbucket[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.SCM.Azure {
		if err := checkDuplicateName("scm.azure_devops", inst.Name, names); err != nil {
			return err
		}
		errs := validateAzureInstance(fmt.Sprintf("scm.azure_devops[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	for i, inst := range c.SCM.SourceHut {
		if err := checkDuplicateName("scm.sourcehut", inst.Name, names); err != nil {
			return err
		}
		errs := validateSourceHutInstance(fmt.Sprintf("scm.sourcehut[%d]", i), inst)
		if len(errs) > 0 {
			return errs[0]
		}
	}

	return nil
}
