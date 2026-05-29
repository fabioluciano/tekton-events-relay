package main

import (
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func TestBuildActionHandlers(t *testing.T) {
	log, _ := zap.NewDevelopment()

	t.Run("GitHub action handlers registered when enabled", func(t *testing.T) {
		cfg := &config.Config{
			Notifiers: config.Notifiers{
				GitHub: &config.GitHubConfig{
					Enabled: true,
					Token:   "test-token",
					BaseURL: "https://api.github.com",
					Actions: &config.GitHubActionsConfig{
						PRComment: &config.ActionCommentConfig{
							Enabled:  true,
							Template: "Test template",
							OnStates: []domain.State{domain.StateSuccess},
						},
						IssueComment: &config.ActionCommentConfig{
							Enabled:  true,
							Template: "Issue template",
							OnStates: []domain.State{domain.StateFailure},
						},
						Label: &config.ActionLabelConfig{
							Enabled:      true,
							SuccessLabel: "ci:passed",
							FailureLabel: "ci:failed",
						},
					},
				},
			},
		}

		reg := buildActionHandlers(cfg, log)
		allHandlers := reg.All()
		githubHandlers := reg.FindByName("github")

		// Should have 4 total handlers for GitHub: status reporter + 3 actions
		if len(githubHandlers) != 4 {
			t.Errorf("expected 4 GitHub handlers (status + 3 actions), got %d", len(githubHandlers))
		}
		if len(allHandlers) != 4 {
			t.Errorf("expected 4 total handlers, got %d", len(allHandlers))
		}
	})

	t.Run("Disabled actions not registered", func(t *testing.T) {
		cfg := &config.Config{
			Notifiers: config.Notifiers{
				GitHub: &config.GitHubConfig{
					Enabled: true,
					Token:   "test-token",
					Actions: &config.GitHubActionsConfig{
						PRComment: &config.ActionCommentConfig{
							Enabled: false,
						},
					},
				},
			},
		}

		reg := buildActionHandlers(cfg, log)
		githubHandlers := reg.FindByName("github")

		// Should have only 1 handler: status reporter (no actions)
		if len(githubHandlers) != 1 {
			t.Errorf("expected 1 GitHub handler (status only), got %d", len(githubHandlers))
		}
	})

	t.Run("Gitea action handlers registered when enabled", func(t *testing.T) {
		cfg := &config.Config{
			Notifiers: config.Notifiers{
				Gitea: &config.GiteaConfig{
					Enabled: true,
					Token:   "gitea-token",
					BaseURL: "https://gitea.example.com",
					Actions: &config.GiteaActionsConfig{
						PRComment: &config.ActionCommentConfig{
							Enabled:  true,
							Template: "Gitea PR comment",
							OnStates: []domain.State{domain.StateSuccess, domain.StateFailure},
						},
					},
				},
			},
		}

		reg := buildActionHandlers(cfg, log)
		giteaHandlers := reg.FindByName("gitea")

		// Should have 2 handlers: legacy status + PR comment action
		if len(giteaHandlers) != 2 {
			t.Errorf("expected 2 Gitea handlers (status + PR comment), got %d", len(giteaHandlers))
		}
	})

	t.Run("GitLab label handler registered when enabled", func(t *testing.T) {
		cfg := &config.Config{
			Notifiers: config.Notifiers{
				GitLabCloud: &config.GitLabConfig{
					Enabled: true,
					Token:   "gitlab-token",
					BaseURL: "https://gitlab.com/api/v4",
					Actions: &config.GitLabActionsConfig{
						Label: &config.ActionLabelConfig{
							Enabled:      true,
							SuccessLabel: "pipeline::success",
							FailureLabel: "pipeline::failed",
						},
					},
				},
			},
		}

		reg := buildActionHandlers(cfg, log)
		gitlabHandlers := reg.FindByName("gitlab-cloud")

		// Should have 2 handlers: legacy status + label action
		if len(gitlabHandlers) != 2 {
			t.Errorf("expected 2 GitLab handlers (status + label), got %d", len(gitlabHandlers))
		}
	})

	t.Run("No handlers when notifier disabled", func(t *testing.T) {
		cfg := &config.Config{
			Notifiers: config.Notifiers{
				GitHub: &config.GitHubConfig{
					Enabled: false,
					Actions: &config.GitHubActionsConfig{
						PRComment: &config.ActionCommentConfig{
							Enabled: true,
						},
					},
				},
			},
		}

		reg := buildActionHandlers(cfg, log)
		allHandlers := reg.All()

		// Should have 0 handlers (notifier disabled)
		if len(allHandlers) != 0 {
			t.Errorf("expected 0 handlers (notifier disabled), got %d", len(allHandlers))
		}
	})

	t.Run("CEL wrapping with valid when expression", func(t *testing.T) {
		cfg := &config.Config{
			Notifiers: config.Notifiers{
				GitHub: &config.GitHubConfig{
					Enabled: true,
					Token:   "test-token",
					BaseURL: "https://api.github.com",
					Actions: &config.GitHubActionsConfig{
						PRComment: &config.ActionCommentConfig{
							Enabled:  true,
							Template: "Test template",
							OnStates: []domain.State{domain.StateSuccess},
							When:     "event.State == 'success'",
						},
					},
				},
			},
		}

		reg := buildActionHandlers(cfg, log)
		githubHandlers := reg.FindByName("github")

		// Should have 2 handlers: status reporter + PR comment (CEL-wrapped)
		if len(githubHandlers) != 2 {
			t.Errorf("expected 2 GitHub handlers (status + CEL-wrapped PR comment), got %d", len(githubHandlers))
		}
	})

	t.Run("CEL wrapping with empty when expression", func(t *testing.T) {
		cfg := &config.Config{
			Notifiers: config.Notifiers{
				GitHub: &config.GitHubConfig{
					Enabled: true,
					Token:   "test-token",
					BaseURL: "https://api.github.com",
					Actions: &config.GitHubActionsConfig{
						Label: &config.ActionLabelConfig{
							Enabled:      true,
							SuccessLabel: "ci:passed",
							FailureLabel: "ci:failed",
							When:         "",
						},
					},
				},
			},
		}

		reg := buildActionHandlers(cfg, log)
		githubHandlers := reg.FindByName("github")

		// Should have 2 handlers: status reporter + label (no CEL wrapper)
		if len(githubHandlers) != 2 {
			t.Errorf("expected 2 GitHub handlers (status + unwrapped label), got %d", len(githubHandlers))
		}
	})
}
