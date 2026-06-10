package config

import (
	"testing"
)

func TestExampleConfigYAML(t *testing.T) {
	cfg, err := Load("../../wiki/examples/config.yaml")
	if err != nil {
		t.Fatalf("Failed to parse examples/config.yaml: %v", err)
	}
	if cfg == nil {
		t.Fatalf("Config is nil")
	}

	// Verify notifiers are present
	notifierCount := 0
	if len(cfg.SCM.GitHub) > 0 {
		notifierCount++
	}
	if len(cfg.SCM.GitLab) > 0 {
		notifierCount++
	}
	if len(cfg.SCM.Gitea) > 0 {
		notifierCount++
	}
	if len(cfg.Notifiers.Slack) > 0 {
		notifierCount++
	}
	if len(cfg.Notifiers.Webhook) > 0 {
		notifierCount++
	}

	if notifierCount == 0 {
		t.Error("Expected at least one notifier configured in examples/config.yaml")
	}

	t.Logf("Successfully parsed examples/config.yaml with %d notifiers", notifierCount)
}

func TestExampleConfigWhenFields(t *testing.T) {
	cfg, err := Load("../../wiki/examples/config.yaml")
	if err != nil {
		t.Fatalf("Failed to parse examples/config.yaml: %v", err)
	}
	if cfg == nil {
		t.Fatalf("Config is nil")
	}

	// Verify when fields are parsed correctly
	// Helper to find action by type in a list
	findAction := func(actions []Action, typ ActionType) *Action {
		for i := range actions {
			if actions[i].Type == typ {
				return &actions[i]
			}
		}
		return nil
	}

	tests := []struct {
		name     string
		whenExpr string
		check    func() string
	}{
		{
			name:     "GitHub PR comment when field",
			whenExpr: `event.Resource == "taskrun" && stateIn("success", "failure")`,
			check: func() string {
				if len(cfg.SCM.GitHub) > 0 {
					if a := findAction(cfg.SCM.GitHub[0].Actions, ActionTypePRComment); a != nil {
						return a.When
					}
				}
				return ""
			},
		},
		{
			name:     "GitHub issue comment when field",
			whenExpr: `event.Namespace == "production" && stateIn("failure", "error")`,
			check: func() string {
				if len(cfg.SCM.GitHub) > 0 {
					if a := findAction(cfg.SCM.GitHub[0].Actions, ActionTypeIssueComment); a != nil {
						return a.When
					}
				}
				return ""
			},
		},
		{
			name:     "Azure DevOps label when field",
			whenExpr: `event.Resource == "pipelinerun" && event.Repo.Owner == "myorg"`,
			check: func() string {
				if len(cfg.SCM.Azure) > 0 {
					if a := findAction(cfg.SCM.Azure[0].Actions, ActionTypeLabel); a != nil {
						return a.When
					}
				}
				return ""
			},
		},
		{
			name:     "Gitea PR comment when field",
			whenExpr: `event.RunName.startsWith("nightly-") && stateIn("success", "failure")`,
			check: func() string {
				if len(cfg.SCM.Gitea) > 0 {
					if a := findAction(cfg.SCM.Gitea[0].Actions, ActionTypePRComment); a != nil {
						return a.When
					}
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
