package config

import (
	"strings"
	"testing"
)

func hasErrContaining(errs []ValidationError, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}

// fixtureTokenFile is a placeholder mount path used only to assert conflict rules.
const fixtureTokenFile = "/etc/secrets/x/token" //nolint:gosec // G101: test fixture path, not a credential

//nolint:gosec // G101: test fixture paths, not real credentials
var validOAuth2 = &OAuth2Config{
	ClientIDFile:     "/etc/secrets/x/client_id",
	ClientSecretFile: "/etc/secrets/x/client_secret",
	TokenURL:         "https://auth.example.com/token",
}

// validateSelf bridges to the package validators for the notifiers that support OAuth2.
func (j JiraInstance) validateSelf() []ValidationError { return validateJiraInstance("jira[0]", j) }

func TestValidateJiraInstance_OAuth2(t *testing.T) {
	base := func(a *JiraAuth) JiraInstance {
		return JiraInstance{Name: "j", Enabled: true, BaseURL: "https://jira.example.com", Auth: a}
	}
	if errs := base(&JiraAuth{OAuth2: validOAuth2}).validateSelf(); len(errs) != 0 {
		t.Fatalf("oauth2-only should be valid, got %v", errs)
	}
	if errs := base(&JiraAuth{Email: "ci@example.com", OAuth2: validOAuth2}).validateSelf(); !hasErrContaining(errs, "cannot use both auth.email") {
		t.Fatalf("email + oauth2 should conflict, got %v", errs)
	}
	if errs := base(&JiraAuth{TokenFile: fixtureTokenFile, OAuth2: validOAuth2}).validateSelf(); !hasErrContaining(errs, "cannot use both auth.token_file") {
		t.Fatalf("token_file + oauth2 should conflict, got %v", errs)
	}
}

func TestValidateJiraInstance_APIVersion(t *testing.T) {
	base := func(v string) JiraInstance {
		return JiraInstance{Name: "j", Enabled: true, BaseURL: "https://jira.example.com", APIVersion: v, Auth: &JiraAuth{OAuth2: validOAuth2}}
	}
	for _, v := range []string{"", "2", "3"} {
		if errs := base(v).validateSelf(); hasErrContaining(errs, "api_version") {
			t.Fatalf("api_version %q should be valid, got %v", v, errs)
		}
	}
	if errs := base("4").validateSelf(); !hasErrContaining(errs, "invalid api_version") {
		t.Fatalf("api_version 4 should be rejected, got %v", errs)
	}
}

func TestValidateOAuth2_GrantType(t *testing.T) {
	// default (empty) and the two headless grants are accepted with token_url.
	for _, gt := range []string{"", OAuth2GrantClientCredentials, OAuth2GrantRefreshToken} {
		o := &OAuth2Config{GrantType: gt, TokenURL: "https://auth.example.com/token"} //nolint:gosec // G101: example token URL, not a credential
		if errs := validateOAuth2("x", o); len(errs) != 0 {
			t.Fatalf("grant_type %q should be valid, got %v", gt, errs)
		}
	}
	// authorization_code (needs a redirect) is rejected.
	o := &OAuth2Config{GrantType: "authorization_code", TokenURL: "https://auth.example.com/token"} //nolint:gosec // G101: example token URL, not a credential
	if errs := validateOAuth2("x", o); !hasErrContaining(errs, "is not supported") {
		t.Fatalf("authorization_code should be rejected, got %v", errs)
	}
	// token_url is always required.
	if errs := validateOAuth2("x", &OAuth2Config{GrantType: OAuth2GrantRefreshToken}); !hasErrContaining(errs, "token_url") {
		t.Fatalf("missing token_url should error, got %v", errs)
	}
}

func TestValidateWebhookAuth_OAuth2(t *testing.T) {
	base := func(a *WebhookAuthConfig) WebhookInstance {
		return WebhookInstance{Name: "w", Enabled: true, URLFile: "/etc/secrets/w/url", Auth: a}
	}
	if errs := validateWebhookAuth("webhook[0]", base(&WebhookAuthConfig{Type: "oauth2", OAuth2: validOAuth2})); len(errs) != 0 {
		t.Fatalf("oauth2 with block should be valid, got %v", errs)
	}
	if errs := validateWebhookAuth("webhook[0]", base(&WebhookAuthConfig{Type: "oauth2"})); !hasErrContaining(errs, "requires an 'oauth2' block") {
		t.Fatalf("oauth2 without block should error, got %v", errs)
	}
	if errs := validateWebhookAuth("webhook[0]", base(&WebhookAuthConfig{Type: "oauth2", TokenFile: "/x", OAuth2: validOAuth2})); !hasErrContaining(errs, "does not accept") {
		t.Fatalf("oauth2 with token_file should error, got %v", errs)
	}
}
