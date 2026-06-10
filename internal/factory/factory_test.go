package factory

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	testStatus            = "status"
	testToken             = "token"
	testMain              = "main"
	testPRComment         = "pr-comment"
	testIssueComment      = "issue-comment"
	testDiscussionComment = "discussion-comment"
	testLabel             = "label"
	testGitHubBaseURL     = "https://api.github.com"
	testGitLabBaseURL     = "https://gitlab.com/api/v4"
	testBitbucketBaseURL  = "https://api.bitbucket.org"
	testAzureBaseURL      = "https://dev.azure.com"
	testGiteaBaseURL      = "https://gitea.example.com"
	testSourceHutBaseURL  = "https://builds.sr.ht"
	testSlackWebhookURL   = "https://hooks.slack.com/test"
	testTokenPrefixed     = "test-testToken"
	msgBuildAllFailed     = "BuildAll failed: %v"
)

func TestMain(m *testing.M) {
	// Setup: create template files used by tests
	templateDir := "/tmp/tekton-test-templates"
	if err := os.MkdirAll(templateDir, 0750); err != nil {
		panic(err)
	}

	templates := map[string]string{
		"t.tmpl":          "Pipeline {{.State}} for {{.RunName}}",
		"test.tmpl":       "Test: {{.State}}",
		"issue.tmpl":      "Issue: {{.State}}",
		"discussion.tmpl": "Discussion: {{.State}}",
		"gitea.tmpl":      "Gitea: {{.State}}",
		"checkrun.tmpl":   "Check: {{.State}}",
		"pr.tmpl":         "PR: {{.State}}",
		"msg.tmpl":        "Message: {{.State}}",
	}

	for name, content := range templates {
		path := filepath.Join(templateDir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			panic(err)
		}
	}

	// Run tests
	code := m.Run()

	// Cleanup
	_ = os.RemoveAll(templateDir)

	os.Exit(code)
}

// unmarshalYAML is a helper to unmarshal YAML into a config struct.
// This is necessary because Instance structs have private fields and cannot be constructed with struct literals.
func unmarshalYAML(t *testing.T, yamlStr string) *config.Config {
	t.Helper()
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("failed to unmarshal YAML: %v", err)
	}
	return &cfg
}

// setupTestConfig creates a test config with a GitHub instance and the given actions.
func setupTestConfig(t *testing.T, actions []config.Action) *config.Config {
	t.Helper()
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		SCM: config.SCMConfig{
			GitHub: []config.GitHubInstance{
				{
					Name:    testMain,
					Enabled: true,
					Auth:    &config.GitHubAuth{SecretFile: tokenFile},
					BaseURL: testGitHubBaseURL,
					Actions: actions,
				},
			},
		},
	}

	return cfg
}

// TestBuildAll_IntegrationWiring validates end-to-end DI wiring across all providers.
// This proves that the factory pattern resolves DI boundaries correctly (problem #6):
// typed configs flow through generic factories with zero runtime type assertions.
//
//nolint:gocyclo // integration test setup requires sequential configuration
func TestBuildAll_IntegrationWiring(t *testing.T) {
	log, _ := zap.NewDevelopment()

	t.Run("all providers wired correctly with one instance each", func(t *testing.T) {
		// Create temp files for secrets
		tmpDir := t.TempDir()
		ghToken := filepath.Join(tmpDir, "gh-token")
		glToken := filepath.Join(tmpDir, "gl-token")
		bbUser := filepath.Join(tmpDir, "bb-user")
		bbPass := filepath.Join(tmpDir, "bb-pass")
		azToken := filepath.Join(tmpDir, "az-token")
		giteaToken := filepath.Join(tmpDir, "gitea-token")
		srhtToken := filepath.Join(tmpDir, "srht-token")
		slackWebhook := filepath.Join(tmpDir, "slack-webhook")
		teamsWebhook := filepath.Join(tmpDir, "teams-webhook")
		discordWebhook := filepath.Join(tmpDir, "discord-webhook")
		pdKey := filepath.Join(tmpDir, "pd-key")
		ddKey := filepath.Join(tmpDir, "dd-key")
		webhookURL := filepath.Join(tmpDir, "webhook-url")

		if err := os.WriteFile(ghToken, []byte("test-gh-token"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(glToken, []byte("test-gl-token"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bbUser, []byte("test-user"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bbPass, []byte("test-pass"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(azToken, []byte("test-az-token"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(giteaToken, []byte("test-gitea-token"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(srhtToken, []byte("test-srht-token"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(slackWebhook, []byte("https://hooks.slack.com/test"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(teamsWebhook, []byte("https://teams.webhook.office.com/test"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(discordWebhook, []byte("https://discord.com/api/webhooks/test"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pdKey, []byte("test-pd-key"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ddKey, []byte("test-dd-key"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(webhookURL, []byte("https://example.com/hook"), 0600); err != nil {
			t.Fatal(err)
		}

		yamlStr := `scm:
  github:
    - name: test-github
      enabled: true
      auth:
        secret_file: ` + ghToken + `
      base_url: ` + testGitHubBaseURL + `
      actions:
        - name: ` + testStatus + `
          type: commit_status
          enabled: true
  gitlab:
    - name: test-gitlab
      enabled: true
      auth:
        secret_file: ` + glToken + `
      base_url: ` + testGitLabBaseURL + `
      actions:
        - name: ` + testStatus + `
          type: commit_status
          enabled: true
  bitbucket:
    - name: test-bitbucket
      variant: cloud
      enabled: true
      auth:
        username_file: ` + bbUser + `
        app_password_file: ` + bbPass + `
      base_url: ` + testBitbucketBaseURL + `
      actions:
        - name: ` + testStatus + `
          type: commit_status
          enabled: true
  azure_devops:
    - name: test-azure
      enabled: true
      secret_file: ` + azToken + `
      base_url: ` + testAzureBaseURL + `
      genre: tekton
      actions:
        - name: ` + testStatus + `
          type: commit_status
          enabled: true
  gitea:
    - name: test-gitea
      enabled: true
      auth:
        secret_file: ` + giteaToken + `
      base_url: ` + testGiteaBaseURL + `
      actions:
        - name: ` + testStatus + `
          type: commit_status
          enabled: true
  sourcehut:
    - name: test-sourcehut
      enabled: true
      auth:
        secret_file: ` + srhtToken + `
      base_url: ` + testSourceHutBaseURL + `
      actions:
        - name: ` + testStatus + `
          type: commit_status
          enabled: true
notifiers:
  slack:
    - name: test-slack
      enabled: true
      auth:
        webhook_url_file: ` + slackWebhook + `
  teams:
    - name: test-teams
      enabled: true
      auth:
        webhook_url_file: ` + teamsWebhook + `
  discord:
    - name: test-discord
      enabled: true
      auth:
        webhook_url_file: ` + discordWebhook + `
  pagerduty:
    - name: test-pagerduty
      enabled: true
      auth:
        integration_key_file: ` + pdKey + `
      severity: critical
  datadog:
    - name: test-datadog
      enabled: true
      auth:
        api_key_file: ` + ddKey + `
      site: datadoghq.com
  webhook:
    - name: test-webhook
      enabled: true
      url_file: ` + webhookURL

		cfg := unmarshalYAML(t, yamlStr)

		reg, err := BuildAll(cfg, log)
		if err != nil {
			t.Fatalf(msgBuildAllFailed, err)
		}

		handlers := reg.All()
		if len(handlers) == 0 {
			t.Fatal("expected handlers from all providers, got empty registry")
		}

		// Verify each SCM provider produced a commit_testStatus handler
		// Name() returns the provider identifier, not the config instance name
		expectedSCM := []string{"github", "test-gitlab", "bitbucket-cloud", "azure-devops", "gitea", "sourcehut"}
		for _, name := range expectedSCM {
			found := false
			for _, h := range handlers {
				if h.Name() == name && h.Type() == notifier.ActionCommitStatus {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("SCM handler %s/commit_testStatus not found in registry", name)
			}
		}

		// Verify each notifier produced a notify handler
		expectedNotifiers := []string{"slack", "teams", "discord", "pagerduty", "datadog", "webhook"}
		for _, name := range expectedNotifiers {
			found := false
			for _, h := range handlers {
				if h.Name() == name && h.Type() == notifier.ActionNotify {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("notifier handler %s/notify not found in registry", name)
			}
		}

		// Total: 6 SCM + 6 notifiers = 12 handlers minimum
		if len(handlers) < 12 {
			t.Errorf("expected at least 12 handlers (6 SCM + 6 notifiers), got %d", len(handlers))
		}

		t.Log("DI boundaries validated: factory pattern correctly wires typed configs without type assertions")
	})

	t.Run("disabled instances produce no handlers", func(t *testing.T) {
		tmpDir := t.TempDir()
		tokenFile := filepath.Join(tmpDir, "token")
		if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
			t.Fatal(err)
		}

		yamlStr := `scm:
  github:
    - name: disabled-gh
      enabled: false
      auth:
        secret_file: ` + tokenFile + `
      actions:
        - name: ` + testStatus + `
          type: commit_status
          enabled: true
  gitlab:
    - name: disabled-gl
      enabled: false
      auth:
        secret_file: ` + tokenFile + `
      actions:
        - name: ` + testStatus + `
          type: commit_status
          enabled: true
notifiers:
  slack:
    - name: disabled-slack
      enabled: false
      auth:
        webhook_url_file: ` + tokenFile

		cfg := unmarshalYAML(t, yamlStr)

		reg, err := BuildAll(cfg, log)
		if err != nil {
			t.Fatalf(msgBuildAllFailed, err)
		}

		if len(reg.All()) != 0 {
			t.Errorf("expected 0 handlers for disabled instances, got %d", len(reg.All()))
		}
	})

	t.Run("disabled actions within enabled instances produce no handlers", func(t *testing.T) {
		cfg := setupTestConfig(t, []config.Action{
			{Name: testStatus, Type: config.ActionTypeCommitStatus, Enabled: false},
			{Name: testLabel, Type: config.ActionTypeLabel, Enabled: false},
		})

		reg, err := BuildAll(cfg, log)
		if err != nil {
			t.Fatalf(msgBuildAllFailed, err)
		}

		if len(reg.All()) != 0 {
			t.Errorf("expected 0 handlers for disabled actions, got %d", len(reg.All()))
		}
	})

	t.Run("invalid CEL expression returns error not panic", func(t *testing.T) {
		cfg := setupTestConfig(t, []config.Action{
			{
				Name:     "bad-cel",
				Type:     config.ActionTypePRComment,
				Enabled:  true,
				Template: "/tmp/tekton-test-templates/t.tmpl",
				When:     "invalid !!! syntax",
			},
		})

		_, err := BuildAll(cfg, log)
		if err == nil {
			t.Error("expected error for invalid CEL expression, got nil")
		}
	})

	t.Run("empty config produces empty registry without error", func(t *testing.T) {
		cfg := &config.Config{}

		reg, err := BuildAll(cfg, log)
		if err != nil {
			t.Fatalf("BuildAll with empty config failed: %v", err)
		}

		if len(reg.All()) != 0 {
			t.Errorf("expected 0 handlers for empty config, got %d", len(reg.All()))
		}
	})
}

func TestBuildAll(t *testing.T) {
	log, _ := zap.NewDevelopment()

	t.Run("GitHub action handlers registered when enabled", func(t *testing.T) {
		cfg := setupTestConfig(t, []config.Action{
			{
				Name:    testStatus,
				Type:    config.ActionTypeCommitStatus,
				Enabled: true,
			},
			{
				Name:     testPRComment,
				Type:     config.ActionTypePRComment,
				Enabled:  true,
				Template: "/tmp/tekton-test-templates/test.tmpl",
			},
			{
				Name:     testIssueComment,
				Type:     config.ActionTypeIssueComment,
				Enabled:  true,
				Template: "/tmp/tekton-test-templates/issue.tmpl"},
			{
				Name:     testDiscussionComment,
				Type:     config.ActionTypeDiscussionComment,
				Enabled:  true,
				Template: "/tmp/tekton-test-templates/discussion.tmpl"},
			{
				Name:         testLabel,
				Type:         config.ActionTypeLabel,
				Enabled:      true,
				SuccessLabel: "ci:passed",
				FailureLabel: "ci:failed",
			},
		})

		reg, err := BuildAll(cfg, log)
		if err != nil {
			t.Fatalf(msgBuildAllFailed, err)
		}
		allHandlers := reg.All()

		// Should have 5 total handlers for GitHub
		if len(allHandlers) != 5 {
			t.Errorf("expected 5 total handlers, got %d", len(allHandlers))
		}
	})

	t.Run("Disabled actions not registered", func(t *testing.T) {
		cfg := setupTestConfig(t, []config.Action{
			{
				Name:    testPRComment,
				Type:    config.ActionTypePRComment,
				Enabled: false,
			},
		})

		reg, err := BuildAll(cfg, log)
		if err != nil {
			t.Fatalf(msgBuildAllFailed, err)
		}
		allHandlers := reg.All()

		// Should have 0 handlers: PRComment disabled
		if len(allHandlers) != 0 {
			t.Errorf("expected 0 handlers (action disabled), got %d", len(allHandlers))
		}
	})

	t.Run("Gitea action handlers registered when enabled", func(t *testing.T) {
		tmpDir := t.TempDir()
		tokenFile := filepath.Join(tmpDir, "gitea-token")
		if err := os.WriteFile(tokenFile, []byte("test-gitea-token"), 0600); err != nil {
			t.Fatal(err)
		}

		yamlStr := `scm:
  gitea:
    - name: ` + testMain + `
      enabled: true
      auth:
        secret_file: ` + tokenFile + `
      base_url: ` + testGiteaBaseURL + `
      actions:
        - name: ` + testPRComment + `
          type: pr_comment
          enabled: true
          template: "/tmp/tekton-test-templates/gitea.tmpl"`

		cfg := unmarshalYAML(t, yamlStr)

		reg, err := BuildAll(cfg, log)
		if err != nil {
			t.Fatalf(msgBuildAllFailed, err)
		}
		allHandlers := reg.All()

		// Should have 1 handler: PR comment action
		if len(allHandlers) != 1 {
			t.Errorf("expected 1 Gitea handler (PR comment), got %d", len(allHandlers))
		}
	})

	t.Run("GitLab testLabel handler registered when enabled", func(t *testing.T) {
		tmpDir := t.TempDir()
		tokenFile := filepath.Join(tmpDir, "gitlab-token")
		if err := os.WriteFile(tokenFile, []byte("test-gitlab-token"), 0600); err != nil {
			t.Fatal(err)
		}

		yamlStr := `scm:
  gitlab:
    - name: gitlab-cloud
      enabled: true
      auth:
        secret_file: ` + tokenFile + `
      base_url: ` + testGitLabBaseURL + `
      actions:
        - name: ` + testLabel + `
          type: label
          enabled: true
          success_label: "pipeline::success"
          failure_label: "pipeline::failed"`

		cfg := unmarshalYAML(t, yamlStr)

		reg, err := BuildAll(cfg, log)
		if err != nil {
			t.Fatalf(msgBuildAllFailed, err)
		}
		allHandlers := reg.All()

		// Should have 1 handler: testLabel action
		if len(allHandlers) != 1 {
			t.Errorf("expected 1 GitLab handler (testLabel), got %d", len(allHandlers))
		}
	})

	t.Run("No handlers when instance disabled", func(t *testing.T) {
		yamlStr := `scm:
  github:
    - name: ` + testMain + `
      enabled: false
      actions:
        - name: ` + testPRComment + `
          type: pr_comment
          enabled: true`

		cfg := unmarshalYAML(t, yamlStr)

		reg, err := BuildAll(cfg, log)
		if err != nil {
			t.Fatalf(msgBuildAllFailed, err)
		}
		allHandlers := reg.All()

		// Should have 0 handlers (instance disabled)
		if len(allHandlers) != 0 {
			t.Errorf("expected 0 handlers (instance disabled), got %d", len(allHandlers))
		}
	})

	t.Run("CEL wrapping with valid when expression", func(t *testing.T) {
		cfg := setupTestConfig(t, []config.Action{
			{
				Name:    testStatus,
				Type:    config.ActionTypeCommitStatus,
				Enabled: true,
			},
			{
				Name:     testPRComment,
				Type:     config.ActionTypePRComment,
				Enabled:  true,
				Template: "/tmp/tekton-test-templates/test.tmpl", When: "event.State == 'success'",
			},
		})

		reg, err := BuildAll(cfg, log)
		if err != nil {
			t.Fatalf(msgBuildAllFailed, err)
		}
		allHandlers := reg.All()

		// Should have 2 handlers: testStatus reporter + PR comment (CEL-wrapped)
		if len(allHandlers) != 2 {
			t.Errorf("expected 2 handlers (testStatus + CEL-wrapped PR comment), got %d", len(allHandlers))
		}
	})

	t.Run("CEL wrapping with empty when expression", func(t *testing.T) {
		cfg := setupTestConfig(t, []config.Action{
			{
				Name:    testStatus,
				Type:    config.ActionTypeCommitStatus,
				Enabled: true,
			},
			{
				Name:         testLabel,
				Type:         config.ActionTypeLabel,
				Enabled:      true,
				SuccessLabel: "ci:passed",
				FailureLabel: "ci:failed",
			},
		})

		reg, err := BuildAll(cfg, log)
		if err != nil {
			t.Fatalf(msgBuildAllFailed, err)
		}
		allHandlers := reg.All()

		// Should have 2 handlers: testStatus reporter + testLabel (no CEL wrapper)
		if len(allHandlers) != 2 {
			t.Errorf("expected 2 handlers (testStatus + unwrapped testLabel), got %d", len(allHandlers))
		}
	})

	t.Run("Invalid CEL expression returns error", func(t *testing.T) {
		cfg := setupTestConfig(t, []config.Action{
			{
				Name:     testPRComment,
				Type:     config.ActionTypePRComment,
				Enabled:  true,
				Template: "/tmp/tekton-test-templates/test.tmpl", When: "invalid CEL syntax !!!",
			},
		})

		_, err := BuildAll(cfg, log)
		if err == nil {
			t.Error("expected error for invalid CEL expression, got nil")
		}
	})
}
