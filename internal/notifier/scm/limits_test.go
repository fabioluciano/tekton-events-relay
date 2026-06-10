package scm

import (
	"strings"
	"testing"
)

const (
	testGitHub          = "github"
	testGitLab          = "gitlab"
	testBitbucketCloud  = "bitbucket-cloud"
	testBitbucketServer = "bitbucket-server"
	testAzureDevOps     = "azure-devops"
	testGitea           = "gitea"
	testSourcehut       = "sourcehut"
	testStatusDesc      = "status_description"
	testCommentBody     = "comment_body"
	testUnknownProvider = "unknown-provider"
	testUnknownField    = "unknown_field"
)

func TestLimits_ProviderKeys(t *testing.T) {
	expectedProviders := []string{
		testGitHub,
		testGitLab,
		testBitbucketCloud,
		testBitbucketServer,
		testAzureDevOps,
		testGitea,
		testSourcehut,
	}

	for _, provider := range expectedProviders {
		if _, exists := Limits[provider]; !exists {
			t.Errorf("Limits map missing provider: %s", provider)
		}
	}

	for provider := range Limits {
		hasUnderscore := strings.Contains(provider, "_")
		if hasUnderscore {
			t.Errorf("Provider key uses underscore (should be hyphen): %s", provider)
		}
	}
}

func TestLimits_BitbucketCloud(t *testing.T) {
	limits, exists := Limits[testBitbucketCloud]
	if !exists {
		t.Fatal(testBitbucketCloud + " not found in Limits map")
	}

	if limits.StatusDescription != 255 {
		t.Errorf("StatusDescription = %d, expected 255", limits.StatusDescription)
	}
	if limits.StatusContext != 255 {
		t.Errorf("StatusContext = %d, expected 255", limits.StatusContext)
	}
	if limits.CommentBody != 65000 {
		t.Errorf("CommentBody = %d, expected 65000", limits.CommentBody)
	}
}

func TestLimits_AzureDevOps(t *testing.T) {
	limits, exists := Limits[testAzureDevOps]
	if !exists {
		t.Fatal(testAzureDevOps + " not found in Limits map")
	}

	if limits.StatusDescription != 4000 {
		t.Errorf("StatusDescription = %d, expected 4000", limits.StatusDescription)
	}
	if limits.CommentBody != 65000 {
		t.Errorf("CommentBody = %d, expected 65000", limits.CommentBody)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		max      int
		expected string
	}{
		{
			name:     "within limit",
			input:    "hello",
			max:      10,
			expected: "hello",
		},
		{
			name:     "exact limit",
			input:    "helloworld",
			max:      10,
			expected: "helloworld",
		},
		{
			name:     "exceeds limit",
			input:    "hello world this is a long string",
			max:      10,
			expected: "hello w...",
		},
		{
			name:     "unlimited (max=0)",
			input:    strings.Repeat("x", 10000),
			max:      0,
			expected: strings.Repeat("x", 10000),
		},
		{
			name:     "very short max",
			input:    "hello",
			max:      2,
			expected: "he",
		},
		{
			name:     "unicode support",
			input:    "こんにちは世界",
			max:      5,
			expected: "こん...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Truncate(tt.input, tt.max)
			if result != tt.expected {
				t.Errorf("Truncate(%q, %d) = %q, expected %q", tt.input, tt.max, result, tt.expected)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		field       string
		value       string
		expectError bool
	}{
		{
			name:        "github status_description within limit",
			provider:    testGitHub,
			field:       testStatusDesc,
			value:       strings.Repeat("x", 140),
			expectError: false,
		},
		{
			name:        "github status_description exceeds limit",
			provider:    testGitHub,
			field:       testStatusDesc,
			value:       strings.Repeat("x", 141),
			expectError: true,
		},
		{
			name:        "sourcehut unlimited (limit=0)",
			provider:    testSourcehut,
			field:       testStatusDesc,
			value:       strings.Repeat("x", 10000),
			expectError: false,
		},
		{
			name:        "unknown provider (no limit)",
			provider:    testUnknownProvider,
			field:       testStatusDesc,
			value:       strings.Repeat("x", 10000),
			expectError: false,
		},
		{
			name:        "unknown field (no limit)",
			provider:    testGitHub,
			field:       testUnknownField,
			value:       strings.Repeat("x", 10000),
			expectError: false,
		},
		{
			name:        "gitlab comment_body within huge limit",
			provider:    testGitLab,
			field:       testCommentBody,
			value:       strings.Repeat("x", 1000000),
			expectError: false,
		},
		{
			name:        "gitlab comment_body exceeds huge limit",
			provider:    testGitLab,
			field:       testCommentBody,
			value:       strings.Repeat("x", 1000001),
			expectError: true,
		},
		{
			name:        "bitbucket-cloud status_context within limit",
			provider:    testBitbucketCloud,
			field:       "status_context",
			value:       strings.Repeat("x", 255),
			expectError: false,
		},
		{
			name:        "azure-devops status_description large limit",
			provider:    testAzureDevOps,
			field:       testStatusDesc,
			value:       strings.Repeat("x", 4000),
			expectError: false,
		},
		{
			name:        "azure-devops status_description exceeds large limit",
			provider:    testAzureDevOps,
			field:       testStatusDesc,
			value:       strings.Repeat("x", 4001),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.provider, tt.field, tt.value)
			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), "exceeds") {
				t.Errorf("error should contain 'exceeds', got: %v", err)
			}
		})
	}
}
