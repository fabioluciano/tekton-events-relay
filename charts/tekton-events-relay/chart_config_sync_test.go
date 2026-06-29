package chart_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHelmSecretRefs_renderNotifierConfigFilePaths(t *testing.T) {
	// Given: representative notifier values using item-level secretRef fields.
	valuesPath := filepath.Join(t.TempDir(), "values.yaml")
	values := `
config:
  notifiers:
    slack:
      - name: slack-main
        enabled: true
        webhook_url:
          secretRef: {name: slack-webhook, key: webhook_url}
    teams:
      - name: teams-main
        enabled: true
        webhook_url:
          secretRef: {name: teams-webhook, key: webhook_url}
    discord:
      - name: discord-main
        enabled: true
        bot_token:
          token:
            secretRef: {name: discord-bot, key: bot_token}
          channel_id: "123456"
    pagerduty:
      - name: pd-main
        enabled: true
        integration_key:
          secretRef: {name: pagerduty-key, key: routing_key}
    datadog:
      - name: dd-main
        enabled: true
        api_key:
          secretRef: {name: datadog-key, key: api_key}
    webhook:
      - name: hook-main
        enabled: true
        url:
          secretRef: {name: webhook-url, key: url}
        auth:
          type: apikey
          token:
            secretRef: {name: webhook-token, key: token}
          header: X-API-Key
    grafana:
      - name: grafana-main
        enabled: true
        url: https://grafana.example.com
        token:
          secretRef: {name: grafana-token, key: token}
    sentry:
      - name: sentry-main
        enabled: true
        org: acme
        token:
          secretRef: {name: sentry-token, key: auth_token}
    email:
      - name: email-main
        enabled: true
        host: smtp.example.com
        username: ci@example.com
        password:
          secretRef: {name: email-password, key: password}
        from: ci@example.com
        to: [team@example.com]
  jira:
    - name: jira-main
      enabled: true
      base_url: https://jira.example.com
      auth:
        token:
          secretRef: {name: jira-token, key: token}
      actions:
        - name: comment
          type: comment
          enabled: true
`
	if err := os.WriteFile(valuesPath, []byte(values), 0600); err != nil {
		t.Fatalf("write values file: %v", err)
	}

	// When: Helm renders the chart exactly as users install it.
	rendered := helmTemplate(t, valuesPath)
	configYAML := renderedConfigYAML(t, rendered)

	// Then: rendered Go config points every notifier credential at the mounted secret path.
	wantPaths := []string{
		`webhook_url_file: "/etc/secrets/slack/slack-main/webhook_url"`,
		`webhook_url_file: "/etc/secrets/teams/teams-main/webhook_url"`,
		`token_file: "/etc/secrets/discord/discord-main/bot_token"`,
		`integration_key_file: "/etc/secrets/pagerduty/pd-main/routing_key"`,
		`api_key_file: "/etc/secrets/datadog/dd-main/api_key"`,
		`url_file: "/etc/secrets/webhook/hook-main/url"`,
		`token_file: "/etc/secrets/webhook/hook-main/token"`,
		`token_file: "/etc/secrets/grafana/grafana-main/token"`,
		`token_file: "/etc/secrets/sentry/sentry-main/auth_token"`,
		`password_file: "/etc/secrets/email/email-main/password"`,
		`token_file: "/etc/secrets/jira/jira-main/token"`,
	}
	for _, want := range wantPaths {
		if !strings.Contains(configYAML, want) {
			t.Fatalf("rendered config.yaml missing %s\nconfig.yaml:\n%s", want, configYAML)
		}
	}

	// And: the Deployment mounts the corresponding secret directories.
	wantMounts := []string{
		`mountPath: /etc/secrets/slack/slack-main`,
		`mountPath: /etc/secrets/teams/teams-main`,
		`mountPath: /etc/secrets/discord/discord-main`,
		`mountPath: /etc/secrets/pagerduty/pd-main`,
		`mountPath: /etc/secrets/datadog/dd-main`,
		`mountPath: /etc/secrets/webhook/hook-main`,
		`mountPath: /etc/secrets/grafana/grafana-main`,
		`mountPath: /etc/secrets/sentry/sentry-main`,
		`mountPath: /etc/secrets/email/email-main`,
		`mountPath: /etc/secrets/jira/jira-main`,
	}
	for _, want := range wantMounts {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered manifests missing %s", want)
		}
	}
}

func helmTemplate(t *testing.T, valuesPath string) string {
	t.Helper()
	cmd := exec.Command("helm", "template", "phase4", ".", "--dependency-update", "-f", valuesPath) //nolint:gosec // G204: test-only Helm invocation
	cmd.Dir = "."
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("helm template failed: %v\nstderr:\n%s", err, stderr.String())
	}
	return string(out)
}

func renderedConfigYAML(t *testing.T, rendered string) string {
	t.Helper()
	for _, doc := range strings.Split(rendered, "---") {
		var manifest struct {
			Kind string            `yaml:"kind"`
			Data map[string]string `yaml:"data"`
		}
		if err := yaml.Unmarshal([]byte(doc), &manifest); err != nil {
			continue
		}
		if manifest.Kind == "ConfigMap" && manifest.Data["config.yaml"] != "" {
			return manifest.Data["config.yaml"]
		}
	}
	t.Fatal("rendered manifests did not include config ConfigMap data")
	return ""
}
