// Package config provides configuration loading, validation, and hot-reload
// for the tekton-events-relay binary.
package config

import (
	"fmt"
	"net/url"
	"text/template"

	"github.com/itchyny/gojq"

	policycel "github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// Common validation rules

// CELCompileFunc validates trusted administrator-configured CEL policy syntax.
//
// The expression surface is intentionally a privileged config surface, not
// untrusted user input: relay operators author these policies, and validation
// rejects malformed expressions before handlers are built. Tests may override
// this hook, but production validation falls back to the real CEL compiler.
var CELCompileFunc = defaultCELCompile

func defaultCELCompile(expr string) error {
	_, err := policycel.Compile(expr)
	return err
}

func validateCELExpression(expr string) error {
	compile := CELCompileFunc
	if compile == nil {
		compile = defaultCELCompile
	}
	return compile(expr)
}

// baseURLProvider is implemented by config instances that have a base URL.
type baseURLProvider interface {
	GetBaseURL() string
}

// actionProvider is implemented by SCM config instances that have actions.
type actionProvider interface {
	GetActions() []Action
}

// celWhenProvider is implemented by notifier config instances that have a CEL when expression.
type celWhenProvider interface {
	GetWhen() string
}

// templateProvider is implemented by notifier config instances that have a Go template.
type templateProvider interface {
	GetTemplate() string
}

func requireBaseURL(prefix string, inst any) []ValidationError {
	type hasEnabled interface {
		isEnabled() bool
	}
	if h, ok := inst.(hasEnabled); !ok || !h.isEnabled() {
		return nil
	}
	if p, ok := inst.(baseURLProvider); ok {
		if p.GetBaseURL() == "" {
			return []ValidationError{{Path: prefix + ".base_url", Message: ValidationMsgBaseURLRequired}}
		}
		if _, err := url.ParseRequestURI(p.GetBaseURL()); err != nil {
			return []ValidationError{{Path: prefix + ".base_url", Message: fmt.Sprintf("invalid URL: %v", err)}}
		}
	}
	return nil
}

func validateActions(prefix string, inst any) []ValidationError {
	p, ok := inst.(actionProvider)
	if !ok {
		return nil
	}
	actions := p.GetActions()
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
			notifier.ActionCommitStatus:      true,
			notifier.ActionCommitComment:     true,
			notifier.ActionPRComment:         true,
			notifier.ActionIssueComment:      true,
			notifier.ActionLabel:             true,
			notifier.ActionDiscussionComment: true,
			notifier.ActionCheckRun:          true,
			notifier.ActionDeploymentStatus:  true,
		}
		if !validTypes[action.Type] {
			errs = append(errs, ValidationError{
				Path:    prefix + ".type",
				Message: fmt.Sprintf("invalid action type '%s' (must be one of: commit_status, commit_comment, pr_comment, issue_comment, label, discussion_comment, check_run, deployment_status)", action.Type),
			})
		}
	}

	// A label action with no effect declared is a configuration mistake.
	if action.Type == notifier.ActionLabel && (action.Labels == nil || (len(action.Labels.Add) == 0 && len(action.Labels.Remove) == 0)) {
		errs = append(errs, ValidationError{
			Path:    prefix + ".labels",
			Message: "label actions require a labels block with at least one add or remove entry",
		})
	}

	if action.When != "" {
		if err := validateCELExpression(action.When); err != nil {
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

func validateCELWhen(prefix string, inst any) []ValidationError {
	p, ok := inst.(celWhenProvider)
	if !ok {
		return nil
	}
	when := p.GetWhen()
	if when != "" {
		if err := validateCELExpression(when); err != nil {
			return []ValidationError{{
				Path:    prefix + ".when",
				Message: fmt.Sprintf("invalid CEL: %v", err),
			}}
		}
	}
	return nil
}

func validateTemplate(prefix string, inst any) []ValidationError {
	p, ok := inst.(templateProvider)
	if !ok {
		return nil
	}
	tmpl := p.GetTemplate()
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

// validateOAuth2 checks the minimal required fields of an oauth2 block for the
// headless grants the relay supports (client_credentials, refresh_token).
func validateOAuth2(prefix string, o *OAuth2Config) []ValidationError {
	if o == nil {
		return nil
	}
	var errs []ValidationError
	switch o.GrantType {
	case "", OAuth2GrantClientCredentials, OAuth2GrantRefreshToken:
		// supported headless grants
	default:
		errs = append(errs, ValidationError{
			Path:    prefix + ".oauth2.grant_type",
			Message: fmt.Sprintf("must be '%s' or '%s' (authorization_code is not supported — the relay has no redirect endpoint; seed a refresh_token instead)", OAuth2GrantClientCredentials, OAuth2GrantRefreshToken),
		})
	}
	if o.TokenURL == "" {
		errs = append(errs, ValidationError{Path: prefix + ".oauth2.token_url", Message: "oauth2 requires 'token_url'"})
	}
	return errs
}

func validateTransform(prefix string, inst any) []ValidationError {
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
		query, err := gojq.Parse(transform)
		if err != nil {
			return []ValidationError{{
				Path:    prefix + ".transform",
				Message: "invalid jq syntax",
			}}
		}
		if _, err := gojq.Compile(query); err != nil {
			return []ValidationError{{
				Path:    prefix + ".transform",
				Message: fmt.Sprintf("invalid jq expression: %v", err),
			}}
		}
	}
	return nil
}
