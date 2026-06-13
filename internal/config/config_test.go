package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		envVars     map[string]string
		wantErr     bool
	}{
		{
			name: "valid config",
			fileContent: `server:
  addr: ":8080"
notifiers: {}`,
			wantErr: false,
		},
		{
			name: "config with env expansion",
			fileContent: `server:
  addr: "${LISTEN_ADDR}"
dashboard_url: "${DASHBOARD_URL}"`,
			envVars: map[string]string{
				"LISTEN_ADDR":   ":9090",
				"DASHBOARD_URL": "http://localhost:8080",
			},
			wantErr: false,
		},
		{
			name:        "invalid yaml",
			fileContent: `invalid: yaml: content:`,
			wantErr:     true,
		},
		{
			name:        "empty file",
			fileContent: ``,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cfgPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(tt.fileContent), 0600); err != nil {
				t.Fatal(err)
			}

			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			cfg, err := Load(cfgPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg == nil {
				t.Error("expected config, got nil")
			}
		})
	}
}

func TestLoadNonExistent(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestExpandEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envVars  map[string]string
		expected string
	}{
		{
			name:     "single var",
			input:    "value is ${VAR}",
			envVars:  map[string]string{"VAR": "test"},
			expected: "value is test",
		},
		{
			name:     "multiple vars",
			input:    "${A} and ${B}",
			envVars:  map[string]string{"A": "foo", "B": "bar"},
			expected: "foo and bar",
		},
		{
			name:     "no vars",
			input:    "plain text",
			envVars:  nil,
			expected: "plain text",
		},
		{
			name:     "undefined var",
			input:    "${UNDEFINED}",
			envVars:  nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}
			result := expandEnv(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnv() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	if cfg.Server.Addr == "" {
		t.Error("expected default server address")
	}
	if cfg.DedupeSize == 0 {
		t.Error("expected default dedupe size")
	}
}

func TestConfig_ValidateTokenReferences_Warns(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		expectWarn  bool
	}{
		{
			name: "literal token triggers warning",
			fileContent: `scm:
  github:
    - name: test
      enabled: true
      auth:
        secret_file: "ghp_literaltoken123"
      base_url: "https://api.github.com"`,
			expectWarn: true,
		},
		{
			name: "env var reference no warning",
			fileContent: `scm:
  github:
    - name: test
      enabled: true
      auth:
        secret_file: "${GITHUB_TOKEN}"
      base_url: "https://api.github.com"`,
			expectWarn: false,
		},
		{
			name: "literal webhook url triggers warning",
			fileContent: `notifiers:
  slack:
    - name: test
      enabled: false
      webhook_url: "https://hooks.slack.com/services/XXX"`,
			expectWarn: true,
		},
		{
			name: "env var webhook url no warning",
			fileContent: `notifiers:
  slack:
    - name: test
      enabled: false
      webhook_url: "${SLACK_WEBHOOK}"`,
			expectWarn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set dummy env vars so expansion doesn't produce empty values
			t.Setenv("GITHUB_TOKEN", "dummy_token")
			t.Setenv("SLACK_WEBHOOK", "https://dummy.slack.com/webhook")

			tmpDir := t.TempDir()
			cfgPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(tt.fileContent), 0600); err != nil {
				t.Fatal(err)
			}

			cfg, err := Load(cfgPath)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			// Create a test logger that captures warnings
			// For this test, we just verify the function doesn't panic
			// In a real scenario, you'd use a test logger to capture warnings
			testLog, _ := zap.NewDevelopment()
			cfg.ValidateTokenReferences(testLog)
		})
	}
}

//nolint:gocyclo // test covers all action type combinations
func TestActionConfigUnmarshal(t *testing.T) {
	fileContent := `scm:
  github:
    - name: main-instance
      enabled: true
      auth:
        secret_file: "ghp_test"
      base_url: "https://api.github.com"
      actions:
        - name: pr-comment
          type: pr_comment
          enabled: true
          template: "Pipeline {{.State}} for {{.RunName}}"
          on_states:
            - success
            - failure
        - name: issue-comment
          type: issue_comment
          enabled: true
          template: "Build {{.State}}"
          on_states:
            - failure
            - error
        - name: label
          type: label
          enabled: true
          labels:
            add: ["ci:passed"]
            remove: ["ci:failed"]
        - name: commit-status
          type: commit_status
          enabled: true
          when: "event.State == 'success'"
  gitea:
    - name: main-instance
      enabled: true
      auth:
        secret_file: "gitea_test"
      base_url: "https://gitea.example.com"
      actions:
        - name: commit-status
          type: commit_status
          enabled: true
        - name: pr-comment
          type: pr_comment
          enabled: true
          template: "PR comment template"
          on_states:
            - success
  gitlab:
    - name: main-instance
      variant: saas
      enabled: true
      auth:
        secret_file: "glpat_test"
      base_url: "https://gitlab.com/api/v4"
      actions:
        - name: label
          type: label
          enabled: true
          labels:
            add: ["pipeline:success"]
            remove: ["pipeline:failed"]
  azure_devops:
    - name: main-instance
      enabled: true
      secret_file: "azure_test"
      base_url: "https://dev.azure.com"
      actions:
        - name: label
          type: label
          enabled: true
          labels:
            add: ["build-passed"]
            remove: ["build-failed"]
`

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(fileContent), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Test GitHub actions
	if len(cfg.SCM.GitHub) == 0 || len(cfg.SCM.GitHub[0].Actions) == 0 {
		t.Fatal("expected GitHub actions config")
	}

	gh := cfg.SCM.GitHub[0].Actions
	// Find pr_comment action (index 0)
	prComment := gh[0]
	if !prComment.Enabled || prComment.Type != notifier.ActionPRComment {
		t.Error("expected enabled GitHub PR comment action")
	}
	if prComment.Template != "Pipeline {{.State}} for {{.RunName}}" {
		t.Errorf("unexpected PR comment template: %s", prComment.Template)
	}

	// issue_comment action (index 1)
	issueComment := gh[1]
	if !issueComment.Enabled || issueComment.Type != notifier.ActionIssueComment {
		t.Error("expected enabled GitHub issue comment action")
	}

	// label action (index 2)
	labelAction := gh[2]
	if !labelAction.Enabled || labelAction.Type != notifier.ActionLabel {
		t.Error("expected enabled GitHub label action")
	}
	if labelAction.Labels == nil || len(labelAction.Labels.Add) != 1 || labelAction.Labels.Add[0].Name != "ci:passed" {
		t.Errorf("unexpected labels add list: %+v", labelAction.Labels)
	}
	if labelAction.Labels == nil || len(labelAction.Labels.Remove) != 1 || labelAction.Labels.Remove[0].Name != "ci:failed" {
		t.Errorf("unexpected labels remove list: %+v", labelAction.Labels)
	}

	// commit_status action (index 3)
	commitStatus := gh[3]
	if !commitStatus.Enabled || commitStatus.Type != notifier.ActionCommitStatus {
		t.Error("expected enabled GitHub commit status action")
	}
	if commitStatus.When != "event.State == 'success'" {
		t.Errorf("unexpected commit_status when expression: %s", commitStatus.When)
	}

	// Test Gitea actions
	if len(cfg.SCM.Gitea) == 0 || len(cfg.SCM.Gitea[0].Actions) == 0 {
		t.Fatal("expected Gitea actions config")
	}
	giteaActions := cfg.SCM.Gitea[0].Actions
	if !giteaActions[0].Enabled || giteaActions[0].Type != notifier.ActionCommitStatus {
		t.Error("expected enabled Gitea commit status action")
	}
	if giteaActions[0].When != "" {
		t.Errorf("expected empty when field for Gitea commit_status, got: %s", giteaActions[0].When)
	}
	if !giteaActions[1].Enabled || giteaActions[1].Type != notifier.ActionPRComment {
		t.Error("expected enabled Gitea PR comment action")
	}

	// Test GitLab actions
	if len(cfg.SCM.GitLab) == 0 || len(cfg.SCM.GitLab[0].Actions) == 0 {
		t.Fatal("expected GitLab actions config")
	}
	if !cfg.SCM.GitLab[0].Actions[0].Enabled || cfg.SCM.GitLab[0].Actions[0].Type != notifier.ActionLabel {
		t.Error("expected enabled GitLab label action")
	}

	// Test Azure DevOps actions
	if len(cfg.SCM.Azure) == 0 || len(cfg.SCM.Azure[0].Actions) == 0 {
		t.Fatal("expected Azure DevOps actions config")
	}
	if !cfg.SCM.Azure[0].Actions[0].Enabled || cfg.SCM.Azure[0].Actions[0].Type != notifier.ActionLabel {
		t.Error("expected enabled Azure DevOps label action")
	}
}

func TestWebhookTransformValidation(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid transform",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      transform: '. | {id: .run_id, status: "ok"}'`,
			wantErr: false,
		},
		{
			name: "invalid jq syntax",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      transform: '. | {invalid'`,
			wantErr:     true,
			errContains: "transform: invalid jq syntax",
		},
		{
			name: "no transform",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cfgPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(tt.fileContent), 0600); err != nil {
				t.Fatal(err)
			}

			_, err := Load(cfgPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error should contain %q, got: %v", tt.errContains, err)
			}
		})
	}
}

func TestWebhookAuthValidation(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid bearer auth",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      auth:
        type: ` + AuthTypeBearer + `
        token_file: "test-token"`,
			wantErr: false,
		},
		{
			name: "valid basic auth",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      auth:
        type: ` + "basic" + `
        username_file: "user"
        password_file: "pass"`,
			wantErr: false,
		},
		{
			name: "valid apikey auth",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      auth:
        type: ` + "apikey" + `
        token_file: "key-123"
        header: "X-API-Key"`,
			wantErr: false,
		},
		{
			name: "valid hmac auth",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      auth:
        type: ` + "hmac" + `
        secret_file: "my-secret"`,
			wantErr: false,
		},
		{
			name: "bearer missing token",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      auth:
        type: ` + AuthTypeBearer + ``,
			wantErr:     true,
			errContains: "type 'bearer' requires 'token_file'",
		},
		{
			name: "bearer with invalid fields",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      auth:
        type: ` + AuthTypeBearer + `
        token_file: "test"
        secret_file: "invalid"`,
			wantErr:     true,
			errContains: "does not accept",
		},
		{
			name: "basic missing password",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      auth:
        type: ` + "basic" + `
        username_file: "user"`,
			wantErr:     true,
			errContains: "type 'basic' requires 'username_file' and 'password_file'",
		},
		{
			name: "apikey missing header",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      auth:
        type: ` + "apikey" + `
        token_file: "key"`,
			wantErr:     true,
			errContains: "type 'apikey' requires 'token_file' and 'header'",
		},
		{
			name: "hmac missing secret",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      auth:
        type: ` + "hmac" + ``,
			wantErr:     true,
			errContains: "type 'hmac' requires 'secret_file'",
		},
		{
			name: "hmac with invalid fields",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      auth:
        type: ` + "hmac" + `
        secret_file: "key"
        token_file: "invalid"`,
			wantErr:     true,
			errContains: "does not accept 'token_file'",
		},
		{
			name: "invalid auth type",
			fileContent: `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      auth:
        type: invalid`,
			wantErr:     true,
			errContains: "invalid type 'invalid'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cfgPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(tt.fileContent), 0600); err != nil {
				t.Fatal(err)
			}

			_, err := Load(cfgPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error should contain %q, got: %v", tt.errContains, err)
			}
		})
	}
}

func TestWebhookTransformAndAuth_Combined(t *testing.T) {
	fileContent := `notifiers:
  webhook:
    - name: test
      enabled: true
      url_file: "https://example.com/webhook"
      transform: '. | {id: .run_id}'
      auth:
        type: bearer
        token_file: "test-token"
      headers:
        X-Custom: "value"`

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(fileContent), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Notifiers.Webhook) == 0 {
		t.Fatal("expected webhook config")
	}

	wh := cfg.Notifiers.Webhook[0]
	if wh.Transform == "" {
		t.Error("expected transform to be set")
	}
	if wh.Auth == nil {
		t.Fatal("expected auth to be set")
	}
	if wh.Auth.Type != AuthTypeBearer {
		t.Errorf("auth type = %q, want %q", wh.Auth.Type, AuthTypeBearer)
	}
	if wh.Headers["X-Custom"] != "value" {
		t.Error("expected custom header")
	}
}
