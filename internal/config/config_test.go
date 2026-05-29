package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
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
			fileContent: `[server]
addr = ":8080"

[notifiers]`,
			wantErr: false,
		},
		{
			name: "config with env expansion",
			fileContent: `[server]
addr = "${LISTEN_ADDR}"

dashboard_url = "${DASHBOARD_URL}"`,
			envVars: map[string]string{
				"LISTEN_ADDR":   ":9090",
				"DASHBOARD_URL": "http://localhost:8080",
			},
			wantErr: false,
		},
		{
			name:        "invalid toml",
			fileContent: `[invalid toml`,
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
			cfgPath := filepath.Join(tmpDir, "config.toml")
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
	_, err := Load("/nonexistent/path/config.toml")
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

func TestActionConfigUnmarshal(t *testing.T) {
	fileContent := `[notifiers.github]
enabled = true
token = "ghp_test"

[notifiers.github.actions.pr_comment]
enabled = true
template = "Pipeline {{.State}} for {{.RunName}}"
on_states = ["success", "failure"]

[notifiers.github.actions.issue_comment]
enabled = true
template = "Build {{.State}}"
on_states = ["failure", "error"]

[notifiers.github.actions.label]
enabled = true
success_label = "ci:passed"
failure_label = "ci:failed"

[notifiers.gitea]
enabled = true
token = "gitea_test"

[notifiers.gitea.actions.pr_comment]
enabled = true
template = "PR comment template"
on_states = ["success"]

[notifiers.gitlab_cloud]
enabled = true
token = "glpat_test"

[notifiers.gitlab_cloud.actions.label]
enabled = true
success_label = "pipeline:success"
failure_label = "pipeline:failed"

[notifiers.azure_devops]
enabled = true
token = "azure_test"

[notifiers.azure_devops.actions.label]
enabled = true
success_label = "build-passed"
failure_label = "build-failed"
`

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(fileContent), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Test GitHub actions
	if cfg.Notifiers.GitHub == nil || cfg.Notifiers.GitHub.Actions == nil {
		t.Fatal("expected GitHub actions config")
	}

	gh := cfg.Notifiers.GitHub.Actions
	if gh.PRComment == nil || !gh.PRComment.Enabled {
		t.Error("expected enabled GitHub PR comment action")
	}
	if gh.PRComment.Template != "Pipeline {{.State}} for {{.RunName}}" {
		t.Errorf("unexpected PR comment template: %s", gh.PRComment.Template)
	}
	if len(gh.PRComment.OnStates) != 2 {
		t.Errorf("expected 2 on_states, got %d", len(gh.PRComment.OnStates))
	}
	if gh.PRComment.OnStates[0] != domain.StateSuccess {
		t.Errorf("expected first state to be success, got %s", gh.PRComment.OnStates[0])
	}
	if gh.PRComment.OnStates[1] != domain.StateFailure {
		t.Errorf("expected second state to be failure, got %s", gh.PRComment.OnStates[1])
	}

	if gh.IssueComment == nil || !gh.IssueComment.Enabled {
		t.Error("expected enabled GitHub issue comment action")
	}
	if len(gh.IssueComment.OnStates) != 2 {
		t.Errorf("expected 2 on_states for issue_comment, got %d", len(gh.IssueComment.OnStates))
	}
	if gh.IssueComment.OnStates[0] != domain.StateFailure {
		t.Errorf("expected first state to be failure, got %s", gh.IssueComment.OnStates[0])
	}

	if gh.Label == nil || !gh.Label.Enabled {
		t.Error("expected enabled GitHub label action")
	}
	if gh.Label.SuccessLabel != "ci:passed" {
		t.Errorf("unexpected success label: %s", gh.Label.SuccessLabel)
	}
	if gh.Label.FailureLabel != "ci:failed" {
		t.Errorf("unexpected failure label: %s", gh.Label.FailureLabel)
	}

	// Test Gitea actions
	if cfg.Notifiers.Gitea == nil || cfg.Notifiers.Gitea.Actions == nil {
		t.Fatal("expected Gitea actions config")
	}
	if cfg.Notifiers.Gitea.Actions.PRComment == nil || !cfg.Notifiers.Gitea.Actions.PRComment.Enabled {
		t.Error("expected enabled Gitea PR comment action")
	}

	// Test GitLab actions
	if cfg.Notifiers.GitLabCloud == nil || cfg.Notifiers.GitLabCloud.Actions == nil {
		t.Fatal("expected GitLab Cloud actions config")
	}
	if cfg.Notifiers.GitLabCloud.Actions.Label == nil || !cfg.Notifiers.GitLabCloud.Actions.Label.Enabled {
		t.Error("expected enabled GitLab label action")
	}

	// Test Azure DevOps actions
	if cfg.Notifiers.AzureDevOps == nil || cfg.Notifiers.AzureDevOps.Actions == nil {
		t.Fatal("expected Azure DevOps actions config")
	}
	if cfg.Notifiers.AzureDevOps.Actions.Label == nil || !cfg.Notifiers.AzureDevOps.Actions.Label.Enabled {
		t.Error("expected enabled Azure DevOps label action")
	}
}
