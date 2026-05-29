package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestExampleConfigTOML(t *testing.T) {
	var cfg Config
	if _, err := toml.DecodeFile("../../examples/config.toml", &cfg); err != nil {
		t.Fatalf("Failed to parse examples/config.toml: %v", err)
	}

	// Verify notifiers are present
	notifierCount := 0
	if cfg.Notifiers.GitHub != nil {
		notifierCount++
	}
	if cfg.Notifiers.GitLabCloud != nil {
		notifierCount++
	}
	if cfg.Notifiers.Gitea != nil {
		notifierCount++
	}
	if cfg.Notifiers.Slack != nil {
		notifierCount++
	}
	if cfg.Notifiers.Webhook != nil {
		notifierCount++
	}

	if notifierCount == 0 {
		t.Error("Expected at least one notifier configured in examples/config.toml")
	}

	t.Logf("Successfully parsed examples/config.toml with %d notifiers", notifierCount)
}

func TestExampleConfigWhenFields(t *testing.T) {
	var cfg Config
	if _, err := toml.DecodeFile("../../examples/config.toml", &cfg); err != nil {
		t.Fatalf("Failed to parse examples/config.toml: %v", err)
	}

	// Verify when fields are parsed correctly
	tests := []struct {
		name     string
		whenExpr string
		check    func() string
	}{
		{
			name:     "GitHub PR comment when field",
			whenExpr: `event.Resource == "taskrun" && event.State == "failure"`,
			check: func() string {
				if cfg.Notifiers.GitHub != nil && cfg.Notifiers.GitHub.Actions != nil &&
					cfg.Notifiers.GitHub.Actions.PRComment != nil {
					return cfg.Notifiers.GitHub.Actions.PRComment.When
				}
				return ""
			},
		},
		{
			name:     "GitHub issue comment when field",
			whenExpr: `event.Namespace == "production"`,
			check: func() string {
				if cfg.Notifiers.GitHub != nil && cfg.Notifiers.GitHub.Actions != nil &&
					cfg.Notifiers.GitHub.Actions.IssueComment != nil {
					return cfg.Notifiers.GitHub.Actions.IssueComment.When
				}
				return ""
			},
		},
		{
			name:     "Azure DevOps label when field",
			whenExpr: `event.Resource == "pipelinerun" && event.Repo.Owner == "myorg"`,
			check: func() string {
				if cfg.Notifiers.AzureDevOps != nil && cfg.Notifiers.AzureDevOps.Actions != nil &&
					cfg.Notifiers.AzureDevOps.Actions.Label != nil {
					return cfg.Notifiers.AzureDevOps.Actions.Label.When
				}
				return ""
			},
		},
		{
			name:     "Gitea PR comment when field",
			whenExpr: `event.RunName.startsWith("nightly-")`,
			check: func() string {
				if cfg.Notifiers.Gitea != nil && cfg.Notifiers.Gitea.Actions != nil &&
					cfg.Notifiers.Gitea.Actions.PRComment != nil {
					return cfg.Notifiers.Gitea.Actions.PRComment.When
				}
				return ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.check()
			if got != tt.whenExpr {
				t.Errorf("Expected when=%q, got %q", tt.whenExpr, got)
			}
		})
	}
}
