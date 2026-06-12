package factory

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
)

// Helper functions to unmarshal SCM instances from YAML since fields are private

func unmarshalGitHubInstance(t *testing.T, yamlStr string) config.GitHubInstance {
	t.Helper()
	var inst config.GitHubInstance
	if err := yaml.Unmarshal([]byte(yamlStr), &inst); err != nil {
		t.Fatalf("failed to unmarshal GitHubInstance: %v", err)
	}
	return inst
}

const (
	testInstanceNameMain     = "main"
	testInstanceNameDisabled = "disabled"
	testTokenValue           = "token"
	testTemplate             = "/tmp/tekton-test-templates/t.tmpl"
	testLabelOK              = "ok"
	testLabelFail            = "fail"
	testInvalidCEL           = notifierBadSyntax
	testActionNameStatus     = "status"
)

// --- GitHub Factory Tests ---

func TestGitHubFactory_Build_returns_nil_when_instance_disabled(t *testing.T) {
	f := &GitHubFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalGitHubInstance(t, `name: `+testInstanceNameDisabled+`
enabled: false
auth:
  secret_file: `+testTokenValue+`
actions:
  - name: s
    type: commit_status
    enabled: true`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for disabled instance, got %d", len(handlers))
	}
}

func assertFactorySkipsDisabledActions(t *testing.T, build func(tokenFile string) (int, error)) {
	t.Helper()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlersCount, err := build(tokenFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlersCount != 0 {
		t.Errorf("expected 0 handlers for disabled actions, got %d", handlersCount)
	}
}

func TestGitHubFactory_Build_skips_disabled_actions(t *testing.T) {
	f := &GitHubFactory{}
	log, _ := zap.NewDevelopment()

	assertFactorySkipsDisabledActions(t, func(tokenFile string) (int, error) {
		handlers, err := f.Build(config.GitHubInstance{
			Name:    testInstanceNameMain,
			Enabled: true,
			Auth:    &config.GitHubAuth{SecretFile: tokenFile},
			BaseURL: testGitHubBaseURL,
			Actions: []config.Action{
				{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: false},
				{Name: "pr", Type: config.ActionTypePRComment, Enabled: false},
			},
		}, log)
		return len(handlers), err
	})
}

func TestGitHubFactory_Build_creates_handler_for_each_action_type(t *testing.T) {
	f := &GitHubFactory{}
	log, _ := zap.NewDevelopment()

	tests := []struct {
		name       string
		actionType config.ActionType
		extra      func(*config.Action)
	}{
		{"commit_status", config.ActionTypeCommitStatus, nil},
		{"pr_comment", config.ActionTypePRComment, func(a *config.Action) {
			a.Template = testTemplate
		}},
		{"issue_comment", config.ActionTypeIssueComment, func(a *config.Action) {
			a.Template = testTemplate
		}},
		{"discussion_comment", config.ActionTypeDiscussionComment, func(a *config.Action) {
			a.Template = testTemplate
		}},
		{"label", config.ActionTypeLabel, func(a *config.Action) { //nolint:goconst
			a.Labels = &config.ActionLabels{Add: []string{"ok"}, Remove: []string{"fail"}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tokenFile := filepath.Join(tmpDir, "token")
			if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
				t.Fatal(err)
			}

			action := config.Action{Name: tt.name, Type: tt.actionType, Enabled: true}
			if tt.extra != nil {
				tt.extra(&action)
			}

			handlers, err := f.Build(config.GitHubInstance{
				Name:    testInstanceNameMain,
				Enabled: true,
				Auth:    &config.GitHubAuth{SecretFile: tokenFile},
				BaseURL: testGitHubBaseURL,
				Actions: []config.Action{action},
			}, log)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(handlers) != 1 {
				t.Errorf("expected 1 handler for %s, got %d", tt.name, len(handlers))
			}
		})
	}
}

func TestGitHubFactory_Build_returns_all_enabled_handlers(t *testing.T) {
	f := &GitHubFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.GitHubInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.GitHubAuth{SecretFile: tokenFile},
		BaseURL: testGitHubBaseURL,
		Actions: []config.Action{
			{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true},
			{Name: "label", Type: config.ActionTypeLabel, Enabled: true, Labels: &config.ActionLabels{Add: []string{"ok"}}},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 2 {
		t.Errorf("expected 2 handlers, got %d", len(handlers))
	}
}

func TestGitHubFactory_Build_skips_unknown_action_type(t *testing.T) {
	f := &GitHubFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.GitHubInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.GitHubAuth{SecretFile: tokenFile},
		BaseURL: testGitHubBaseURL,
		Actions: []config.Action{
			{Name: "unknown", Type: "nonexistent_type", Enabled: true}, //nolint:goconst
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 0 {
		t.Errorf("expected 0 handlers for unknown type, got %d", len(handlers))
	}
}

func TestGitHubFactory_Build_wraps_handler_with_CEL_when_present(t *testing.T) {
	f := &GitHubFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.GitHubInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.GitHubAuth{SecretFile: tokenFile},
		BaseURL: testGitHubBaseURL,
		Actions: []config.Action{
			{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true, When: "event.State == 'success'"},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 CEL-wrapped handler, got %d", len(handlers))
	}
}

func TestGitHubFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &GitHubFactory{}
	log, _ := zap.NewDevelopment()

	_, err := f.Build(config.GitHubInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.GitHubAuth{SecretFile: testTokenValue},
		BaseURL: testGitHubBaseURL,
		Actions: []config.Action{
			{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true, When: "invalid CEL !!!"},
		},
	}, log)

	if err == nil {
		t.Error("expected error for invalid CEL expression, got nil")
	}
}

func TestGitHubFactory_Build_wraps_handler_with_filter_when_present(t *testing.T) {
	f := &GitHubFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.GitHubInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.GitHubAuth{SecretFile: tokenFile},
		BaseURL: testGitHubBaseURL,
		Actions: []config.Action{
			{
				Name:    "s",
				Type:    config.ActionTypeCommitStatus,
				Enabled: true,
				Filter: &config.ActionFilterConfig{
					Tasks: config.FilterList{Allow: []string{"build-*"}},
				},
			},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 filter-wrapped handler, got %d", len(handlers))
	}
}

// --- GitLab Factory Tests ---

func TestGitLabFactory_Build_returns_nil_when_instance_disabled(t *testing.T) {
	f := &GitLabFactory{}
	log, _ := zap.NewDevelopment()

	handlers, err := f.Build(config.GitLabInstance{
		Name:    testInstanceNameDisabled,
		Enabled: false,
		Auth:    &config.GitLabAuth{SecretFile: testTokenValue},
		Actions: []config.Action{{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true}},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for disabled instance, got %d", len(handlers))
	}
}

func TestGitLabFactory_Build_creates_commit_status_handler(t *testing.T) {
	f := &GitLabFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.GitLabInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.GitLabAuth{SecretFile: tokenFile},
		BaseURL: testGitLabBaseURL,
		Actions: []config.Action{
			{Name: "status", Type: config.ActionTypeCommitStatus, Enabled: true}, //nolint:goconst
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 handler, got %d", len(handlers))
	}
}

func TestGitLabFactory_Build_creates_label_handler(t *testing.T) {
	f := &GitLabFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.GitLabInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.GitLabAuth{SecretFile: tokenFile},
		BaseURL: testGitLabBaseURL,
		Actions: []config.Action{
			{Name: "label", Type: config.ActionTypeLabel, Enabled: true, Labels: &config.ActionLabels{Add: []string{"ok"}}},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 label handler, got %d", len(handlers))
	}
}

func TestGitLabFactory_Build_skips_disabled_actions(t *testing.T) {
	f := &GitLabFactory{}
	log, _ := zap.NewDevelopment()

	assertFactorySkipsDisabledActions(t, func(tokenFile string) (int, error) {
		handlers, err := f.Build(config.GitLabInstance{
			Name:    testInstanceNameMain,
			Enabled: true,
			Auth:    &config.GitLabAuth{SecretFile: tokenFile},
			BaseURL: testGitLabBaseURL,
			Actions: []config.Action{
				{Name: "status", Type: config.ActionTypeCommitStatus, Enabled: false},
				{Name: "label", Type: config.ActionTypeLabel, Enabled: false},
			},
		}, log)
		return len(handlers), err
	})
}

func TestGitLabFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &GitLabFactory{}
	log, _ := zap.NewDevelopment()

	_, err := f.Build(config.GitLabInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.GitLabAuth{SecretFile: testTokenValue},
		BaseURL: testGitLabBaseURL,
		Actions: []config.Action{
			{Name: "status", Type: config.ActionTypeCommitStatus, Enabled: true, When: notifierBadSyntax},
		},
	}, log)

	if err == nil {
		t.Error("expected error for invalid CEL expression, got nil")
	}
}

func TestGitLabFactory_Build_skips_unknown_action_type(t *testing.T) {
	f := &GitLabFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.GitLabInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.GitLabAuth{SecretFile: tokenFile},
		BaseURL: testGitLabBaseURL,
		Actions: []config.Action{
			{Name: "x", Type: "nonexistent_type", Enabled: true},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 0 {
		t.Errorf("expected 0 handlers for unknown type, got %d", len(handlers))
	}
}

// --- Bitbucket Factory Tests ---

func TestBitbucketFactory_Build_returns_nil_when_instance_disabled(t *testing.T) {
	f := &BitbucketFactory{}
	log, _ := zap.NewDevelopment()

	handlers, err := f.Build(config.BitbucketInstance{
		Name:    testInstanceNameDisabled,
		Enabled: false,
		Actions: []config.Action{{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true}},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for disabled instance, got %d", len(handlers))
	}
}

func TestBitbucketFactory_Build_creates_cloud_status_handler(t *testing.T) {
	f := &BitbucketFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	userFile := filepath.Join(tmpDir, "user")
	passFile := filepath.Join(tmpDir, "pass")
	if err := os.WriteFile(userFile, []byte("test-user"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passFile, []byte("test-pass"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.BitbucketInstance{
		Name:    "cloud", //nolint:goconst
		Enabled: true,
		Variant: "cloud",
		Auth:    &config.BitbucketAuth{UsernameFile: userFile, AppPasswordFile: passFile},
		BaseURL: "https://api.bitbucket.org/2.0",
		Actions: []config.Action{
			{Name: "status", Type: config.ActionTypeCommitStatus, Enabled: true},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 cloud status handler, got %d", len(handlers))
	}
}

func TestBitbucketFactory_Build_creates_server_status_handler(t *testing.T) {
	f := &BitbucketFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.BitbucketInstance{
		Name:    "server",
		Enabled: true,
		Variant: "server",
		Auth:    &config.BitbucketAuth{TokenFile: tokenFile},
		BaseURL: "https://bitbucket.example.com",
		Actions: []config.Action{
			{Name: "status", Type: config.ActionTypeCommitStatus, Enabled: true},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 server status handler, got %d", len(handlers))
	}
}

func TestBitbucketFactory_Build_creates_cloud_pr_comment_handler(t *testing.T) {
	f := &BitbucketFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	userFile := filepath.Join(tmpDir, "user")
	passFile := filepath.Join(tmpDir, "pass")
	if err := os.WriteFile(userFile, []byte("test-user"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passFile, []byte("test-pass"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.BitbucketInstance{
		Name:    "cloud",
		Enabled: true,
		Variant: "cloud",
		Auth:    &config.BitbucketAuth{UsernameFile: userFile, AppPasswordFile: passFile},
		BaseURL: "https://api.bitbucket.org/2.0",
		Actions: []config.Action{},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 0 {
		t.Errorf("expected 0 handlers for empty Actions, got %d", len(handlers))
	}
}

func TestBitbucketFactory_Build_creates_server_pr_comment_handler(t *testing.T) {
	f := &BitbucketFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.BitbucketInstance{
		Name:    "server",
		Enabled: true,
		Variant: "server",
		Auth:    &config.BitbucketAuth{TokenFile: tokenFile},
		BaseURL: "https://bitbucket.example.com",
		Actions: []config.Action{},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 0 {
		t.Errorf("expected 0 handlers for empty Actions, got %d", len(handlers))
	}
}

func TestBitbucketFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &BitbucketFactory{}
	log, _ := zap.NewDevelopment()

	_, err := f.Build(config.BitbucketInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Variant: "cloud",
		BaseURL: "https://api.bitbucket.org/2.0",
		Actions: []config.Action{
			{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true, When: notifierBadSyntax},
		},
	}, log)

	if err == nil {
		t.Error("expected error for invalid CEL expression, got nil")
	}
}

func TestBitbucketFactory_Build_skips_unknown_action_type(t *testing.T) {
	f := &BitbucketFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	userFile := filepath.Join(tmpDir, "user")
	passFile := filepath.Join(tmpDir, "pass")
	if err := os.WriteFile(userFile, []byte("test-user"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passFile, []byte("test-pass"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.BitbucketInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Variant: "cloud",
		Auth:    &config.BitbucketAuth{UsernameFile: userFile, AppPasswordFile: passFile},
		BaseURL: "https://api.bitbucket.org/2.0",
		Actions: []config.Action{
			{Name: "x", Type: "nonexistent_type", Enabled: true},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 0 {
		t.Errorf("expected 0 handlers for unknown type, got %d", len(handlers))
	}
}

// --- Azure Factory Tests ---

func TestAzureFactory_Build_returns_nil_when_instance_disabled(t *testing.T) {
	f := &AzureFactory{}
	log, _ := zap.NewDevelopment()

	handlers, err := f.Build(config.AzureInstance{
		Name:    testInstanceNameDisabled,
		Enabled: false,
		Actions: []config.Action{{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true}},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for disabled instance, got %d", len(handlers))
	}
}

func TestAzureFactory_Build_creates_commit_status_handler(t *testing.T) {
	f := &AzureFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.AzureInstance{
		Name:       testInstanceNameMain,
		Enabled:    true,
		SecretFile: tokenFile,
		BaseURL:    testAzureBaseURL,
		Genre:      "ci",
		Actions: []config.Action{
			{Name: "status", Type: config.ActionTypeCommitStatus, Enabled: true},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 status handler, got %d", len(handlers))
	}
}

func TestAzureFactory_Build_creates_pr_comment_handler(t *testing.T) {
	f := &AzureFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.AzureInstance{
		Name:       testInstanceNameMain,
		Enabled:    true,
		SecretFile: tokenFile,
		BaseURL:    testAzureBaseURL,
		Genre:      "ci",
		Actions:    []config.Action{},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 0 {
		t.Errorf("expected 0 handlers for empty Actions, got %d", len(handlers))
	}
}

func TestAzureFactory_Build_creates_label_handler(t *testing.T) {
	f := &AzureFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.AzureInstance{
		Name:       testInstanceNameMain,
		Enabled:    true,
		SecretFile: tokenFile,
		BaseURL:    testAzureBaseURL,
		Genre:      "ci",
		Actions: []config.Action{
			{Name: "label", Type: config.ActionTypeLabel, Enabled: true, Labels: &config.ActionLabels{Add: []string{"ok"}}},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 label handler, got %d", len(handlers))
	}
}

func TestAzureFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &AzureFactory{}
	log, _ := zap.NewDevelopment()

	_, err := f.Build(config.AzureInstance{
		Name:       testInstanceNameMain,
		Enabled:    true,
		SecretFile: testTokenValue,
		BaseURL:    testAzureBaseURL,
		Actions: []config.Action{
			{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true, When: notifierBadSyntax},
		},
	}, log)

	if err == nil {
		t.Error("expected error for invalid CEL expression, got nil")
	}
}

func TestAzureFactory_Build_skips_unknown_action_type(t *testing.T) {
	f := &AzureFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.AzureInstance{
		Name:       testInstanceNameMain,
		Enabled:    true,
		SecretFile: tokenFile,
		BaseURL:    testAzureBaseURL,
		Actions: []config.Action{
			{Name: "x", Type: "nonexistent_type", Enabled: true},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 0 {
		t.Errorf("expected 0 handlers for unknown type, got %d", len(handlers))
	}
}

// --- Gitea Factory Tests ---

func TestGiteaFactory_Build_returns_nil_when_instance_disabled(t *testing.T) {
	f := &GiteaFactory{}
	log, _ := zap.NewDevelopment()

	handlers, err := f.Build(config.GiteaInstance{
		Name:    testInstanceNameDisabled,
		Enabled: false,
		Actions: []config.Action{{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true}},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for disabled instance, got %d", len(handlers))
	}
}

func TestGiteaFactory_Build_creates_handler_for_each_action_type(t *testing.T) {
	f := &GiteaFactory{}
	log, _ := zap.NewDevelopment()

	tests := []struct {
		name       string
		actionType config.ActionType
		extra      func(*config.Action)
	}{
		{"commit_status", config.ActionTypeCommitStatus, nil},
		{"pr_comment", config.ActionTypePRComment, func(a *config.Action) {
			a.Template = testTemplate
		}},
		{"issue_comment", config.ActionTypeIssueComment, func(a *config.Action) {
			a.Template = testTemplate
		}},
		{"label", config.ActionTypeLabel, func(a *config.Action) {
			a.Labels = &config.ActionLabels{Add: []string{"ok"}, Remove: []string{"fail"}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tokenFile := filepath.Join(tmpDir, "token")
			if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
				t.Fatal(err)
			}

			action := config.Action{Name: tt.name, Type: tt.actionType, Enabled: true}
			if tt.extra != nil {
				tt.extra(&action)
			}

			handlers, err := f.Build(config.GiteaInstance{
				Name:    testInstanceNameMain,
				Enabled: true,
				Auth:    &config.GiteaAuth{SecretFile: tokenFile},
				BaseURL: "https://gitea.example.com",
				Actions: []config.Action{action},
			}, log)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(handlers) != 1 {
				t.Errorf("expected 1 handler for %s, got %d", tt.name, len(handlers))
			}
		})
	}
}

func TestGiteaFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &GiteaFactory{}
	log, _ := zap.NewDevelopment()

	_, err := f.Build(config.GiteaInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.GiteaAuth{SecretFile: testTokenValue},
		BaseURL: "https://gitea.example.com",
		Actions: []config.Action{
			{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true, When: notifierBadSyntax},
		},
	}, log)

	if err == nil {
		t.Error("expected error for invalid CEL expression, got nil")
	}
}

func TestGiteaFactory_Build_skips_unknown_action_type(t *testing.T) {
	f := &GiteaFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.GiteaInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.GiteaAuth{SecretFile: tokenFile},
		BaseURL: "https://gitea.example.com",
		Actions: []config.Action{
			{Name: "x", Type: "nonexistent_type", Enabled: true},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 0 {
		t.Errorf("expected 0 handlers for unknown type, got %d", len(handlers))
	}
}

// --- SourceHut Factory Tests ---

func TestSourceHutFactory_Build_returns_nil_when_instance_disabled(t *testing.T) {
	f := &SourceHutFactory{}
	log, _ := zap.NewDevelopment()

	handlers, err := f.Build(config.SourceHutInstance{
		Name:    testInstanceNameDisabled,
		Enabled: false,
		Actions: []config.Action{{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true}},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for disabled instance, got %d", len(handlers))
	}
}

func TestSourceHutFactory_Build_creates_commit_status_handler(t *testing.T) {
	f := &SourceHutFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.SourceHutInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.SourceHutAuth{SecretFile: tokenFile},
		BaseURL: "https://builds.sr.ht",
		Actions: []config.Action{
			{Name: "status", Type: config.ActionTypeCommitStatus, Enabled: true},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 status handler, got %d", len(handlers))
	}
}

func TestSourceHutFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &SourceHutFactory{}
	log, _ := zap.NewDevelopment()

	_, err := f.Build(config.SourceHutInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.SourceHutAuth{SecretFile: testTokenValue},
		BaseURL: "https://builds.sr.ht",
		Actions: []config.Action{
			{Name: "s", Type: config.ActionTypeCommitStatus, Enabled: true, When: notifierBadSyntax},
		},
	}, log)

	if err == nil {
		t.Error("expected error for invalid CEL expression, got nil")
	}
}

func TestSourceHutFactory_Build_skips_unsupported_action_type(t *testing.T) {
	f := &SourceHutFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	handlers, err := f.Build(config.SourceHutInstance{
		Name:    testInstanceNameMain,
		Enabled: true,
		Auth:    &config.SourceHutAuth{SecretFile: tokenFile},
		BaseURL: "https://builds.sr.ht",
		Actions: []config.Action{
			{Name: "pr", Type: config.ActionTypePRComment, Enabled: true},
		},
	}, log)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(handlers) != 0 {
		t.Errorf("expected 0 handlers for unsupported type, got %d", len(handlers))
	}
}
