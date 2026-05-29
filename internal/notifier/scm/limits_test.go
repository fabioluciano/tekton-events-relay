package scm

import (
	"strings"
	"testing"
)

func TestLimits_ProviderKeys(t *testing.T) {
	expectedProviders := []string{
		"github",
		"gitlab",
		"bitbucket-cloud",
		"bitbucket-server",
		"azure-devops",
		"gitea",
		"sourcehut",
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
	limits, exists := Limits["bitbucket-cloud"]
	if !exists {
		t.Fatal("bitbucket-cloud not found in Limits map")
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
	limits, exists := Limits["azure-devops"]
	if !exists {
		t.Fatal("azure-devops not found in Limits map")
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
			provider:    "github",
			field:       "status_description",
			value:       strings.Repeat("x", 140),
			expectError: false,
		},
		{
			name:        "github status_description exceeds limit",
			provider:    "github",
			field:       "status_description",
			value:       strings.Repeat("x", 141),
			expectError: true,
		},
		{
			name:        "sourcehut unlimited (limit=0)",
			provider:    "sourcehut",
			field:       "status_description",
			value:       strings.Repeat("x", 10000),
			expectError: false,
		},
		{
			name:        "unknown provider (no limit)",
			provider:    "unknown-provider",
			field:       "status_description",
			value:       strings.Repeat("x", 10000),
			expectError: false,
		},
		{
			name:        "unknown field (no limit)",
			provider:    "github",
			field:       "unknown_field",
			value:       strings.Repeat("x", 10000),
			expectError: false,
		},
		{
			name:        "gitlab comment_body within huge limit",
			provider:    "gitlab",
			field:       "comment_body",
			value:       strings.Repeat("x", 1000000),
			expectError: false,
		},
		{
			name:        "gitlab comment_body exceeds huge limit",
			provider:    "gitlab",
			field:       "comment_body",
			value:       strings.Repeat("x", 1000001),
			expectError: true,
		},
		{
			name:        "bitbucket-cloud status_context within limit",
			provider:    "bitbucket-cloud",
			field:       "status_context",
			value:       strings.Repeat("x", 255),
			expectError: false,
		},
		{
			name:        "azure-devops status_description large limit",
			provider:    "azure-devops",
			field:       "status_description",
			value:       strings.Repeat("x", 4000),
			expectError: false,
		},
		{
			name:        "azure-devops status_description exceeds large limit",
			provider:    "azure-devops",
			field:       "status_description",
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

func TestValidateAndTruncate(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		field    string
		value    string
		validate func(t *testing.T, result string)
	}{
		{
			name:     "github status_description within limit",
			provider: "github",
			field:    "status_description",
			value:    strings.Repeat("x", 140),
			validate: func(t *testing.T, result string) {
				if len(result) != 140 {
					t.Errorf("expected length 140, got %d", len(result))
				}
			},
		},
		{
			name:     "github status_description exceeds limit",
			provider: "github",
			field:    "status_description",
			value:    strings.Repeat("x", 200),
			validate: func(t *testing.T, result string) {
				if len(result) != 140 {
					t.Errorf("expected truncated to 140, got %d", len(result))
				}
				if !strings.HasSuffix(result, "...") {
					t.Errorf("expected result to end with '...', got %q", result)
				}
			},
		},
		{
			name:     "sourcehut unlimited",
			provider: "sourcehut",
			field:    "status_description",
			value:    strings.Repeat("x", 10000),
			validate: func(t *testing.T, result string) {
				if len(result) != 10000 {
					t.Errorf("expected no truncation for sourcehut, got %d chars", len(result))
				}
			},
		},
		{
			name:     "unknown provider",
			provider: "unknown-provider",
			field:    "status_description",
			value:    strings.Repeat("x", 10000),
			validate: func(t *testing.T, result string) {
				if result != strings.Repeat("x", 10000) {
					t.Errorf("expected no truncation for unknown provider")
				}
			},
		},
		{
			name:     "unknown field",
			provider: "github",
			field:    "unknown_field",
			value:    strings.Repeat("x", 10000),
			validate: func(t *testing.T, result string) {
				if result != strings.Repeat("x", 10000) {
					t.Errorf("expected no truncation for unknown field")
				}
			},
		},
		{
			name:     "bitbucket-cloud status_context",
			provider: "bitbucket-cloud",
			field:    "status_context",
			value:    strings.Repeat("x", 300),
			validate: func(t *testing.T, result string) {
				if len(result) != 255 {
					t.Errorf("expected truncated to 255, got %d", len(result))
				}
			},
		},
		{
			name:     "azure-devops status_description large limit",
			provider: "azure-devops",
			field:    "status_description",
			value:    strings.Repeat("x", 5000),
			validate: func(t *testing.T, result string) {
				if len(result) != 4000 {
					t.Errorf("expected truncated to 4000, got %d", len(result))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateAndTruncate(tt.provider, tt.field, tt.value)
			tt.validate(t, result)
		})
	}
}
