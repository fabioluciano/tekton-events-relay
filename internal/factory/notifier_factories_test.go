package factory

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
)

// Helper functions to unmarshal individual instances from YAML since fields are private

func unmarshalSlackInstance(t *testing.T, yamlStr string) config.SlackInstance {
	t.Helper()
	var inst config.SlackInstance
	if err := yaml.Unmarshal([]byte(yamlStr), &inst); err != nil {
		t.Fatalf("failed to unmarshal SlackInstance: %v", err)
	}
	return inst
}

func unmarshalTeamsInstance(t *testing.T, yamlStr string) config.TeamsInstance {
	t.Helper()
	var inst config.TeamsInstance
	if err := yaml.Unmarshal([]byte(yamlStr), &inst); err != nil {
		t.Fatalf("failed to unmarshal TeamsInstance: %v", err)
	}
	return inst
}

func unmarshalDiscordInstance(t *testing.T, yamlStr string) config.DiscordInstance {
	t.Helper()
	var inst config.DiscordInstance
	if err := yaml.Unmarshal([]byte(yamlStr), &inst); err != nil {
		t.Fatalf("failed to unmarshal DiscordInstance: %v", err)
	}
	return inst
}

func unmarshalDatadogInstance(t *testing.T, yamlStr string) config.DatadogInstance {
	t.Helper()
	var inst config.DatadogInstance
	if err := yaml.Unmarshal([]byte(yamlStr), &inst); err != nil {
		t.Fatalf("failed to unmarshal DatadogInstance: %v", err)
	}
	return inst
}

func unmarshalPagerDutyInstance(t *testing.T, yamlStr string) config.PagerDutyInstance {
	t.Helper()
	var inst config.PagerDutyInstance
	if err := yaml.Unmarshal([]byte(yamlStr), &inst); err != nil {
		t.Fatalf("failed to unmarshal PagerDutyInstance: %v", err)
	}
	return inst
}

func unmarshalWebhookInstance(t *testing.T, yamlStr string) config.WebhookInstance {
	t.Helper()
	var inst config.WebhookInstance
	if err := yaml.Unmarshal([]byte(yamlStr), &inst); err != nil {
		t.Fatalf("failed to unmarshal WebhookInstance: %v", err)
	}
	return inst
}

const (
	notifierMain             = "main"
	notifierDisabled         = "disabled"
	notifierUnexpectedError  = "unexpected error: %v"
	notifierExpectedNil      = "expected nil handlers for disabled instance, got %d"
	notifierExpectedOne      = "expected 1 handler, got %d"
	notifierExpectedCELError = "expected CEL compilation error, got nil"
	notifierBadSyntax        = "bad syntax !!!"
	notifierKey              = "key"
)

// --- Slack Factory Tests ---

func TestSlackFactory_Build_returns_nil_when_instance_notifierDisabled(t *testing.T) {
	f := &SlackFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalSlackInstance(t, `name: `+notifierDisabled+`
enabled: false
auth:
  webhook_url_file: https://hooks.slack.com/test`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for notifierDisabled instance, got %d", len(handlers))
	}
}

func TestSlackFactory_Build_creates_handler_when_enabled(t *testing.T) {
	f := &SlackFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	webhookFile := filepath.Join(tmpDir, "webhook")
	if err := os.WriteFile(webhookFile, []byte("https://hooks.slack.com/test"), 0600); err != nil {
		t.Fatal(err)
	}

	inst := unmarshalSlackInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  webhook_url_file: `+webhookFile+`
channel: "#builds"
username: ci-bot
icon_emoji: ":robot:"`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if len(handlers) != 1 {
		t.Errorf(notifierExpectedOne, len(handlers))
	}
}

func TestSlackFactory_Build_bot_token_creates_handler(t *testing.T) {
	f := &SlackFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("xoxb-test-token"), 0600); err != nil {
		t.Fatal(err)
	}

	inst := unmarshalSlackInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  bot_token:
    token_file: `+tokenFile+`
    channel_id: C12345`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if len(handlers) != 1 {
		t.Errorf(notifierExpectedOne, len(handlers))
	}
}

func TestSlackFactory_Build_wraps_handler_with_CEL_when_present(t *testing.T) {
	f := &SlackFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	webhookFile := filepath.Join(tmpDir, "webhook")
	if err := os.WriteFile(webhookFile, []byte("https://hooks.slack.com/test"), 0600); err != nil {
		t.Fatal(err)
	}

	inst := unmarshalSlackInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  webhook_url_file: `+webhookFile+`
when: "event.State == 'failure'"`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 CEL-wrapped handler, got %d", len(handlers))
	}
}

func TestSlackFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &SlackFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalSlackInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  webhook_url_file: https://hooks.slack.com/test
when: "`+notifierBadSyntax+`"`)

	_, err := f.Build(inst, log)

	if err == nil {
		t.Error("notifierExpectedCELError")
	}
}

// --- Teams Factory Tests ---

func TestTeamsFactory_Build_returns_nil_when_instance_notifierDisabled(t *testing.T) {
	f := &TeamsFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalTeamsInstance(t, `name: `+notifierDisabled+`
enabled: false
auth:
  webhook_url_file: https://outlook.webhook.office.com/test`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for notifierDisabled instance, got %d", len(handlers))
	}
}

func TestTeamsFactory_Build_creates_handler_when_enabled(t *testing.T) {
	f := &TeamsFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	webhookFile := filepath.Join(tmpDir, "webhook")
	if err := os.WriteFile(webhookFile, []byte("https://outlook.webhook.office.com/test"), 0600); err != nil {
		t.Fatal(err)
	}

	inst := unmarshalTeamsInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  webhook_url_file: `+webhookFile)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if len(handlers) != 1 {
		t.Errorf(notifierExpectedOne, len(handlers))
	}
}

func TestTeamsFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &TeamsFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalTeamsInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  webhook_url_file: https://outlook.webhook.office.com/test
when: "`+notifierBadSyntax+`"`)

	_, err := f.Build(inst, log)

	if err == nil {
		t.Error("notifierExpectedCELError")
	}
}

// --- Discord Factory Tests ---

func TestDiscordFactory_Build_returns_nil_when_instance_notifierDisabled(t *testing.T) {
	f := &DiscordFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalDiscordInstance(t, `name: `+notifierDisabled+`
enabled: false
auth:
  webhook_url_file: https://discord.com/api/webhooks/test`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for notifierDisabled instance, got %d", len(handlers))
	}
}

func TestDiscordFactory_Build_creates_handler_when_enabled(t *testing.T) {
	f := &DiscordFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	webhookFile := filepath.Join(tmpDir, "webhook")
	if err := os.WriteFile(webhookFile, []byte("https://discord.com/api/webhooks/123/abc"), 0600); err != nil {
		t.Fatal(err)
	}

	inst := unmarshalDiscordInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  webhook_url_file: `+webhookFile+`
username: CI Bot`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if len(handlers) != 1 {
		t.Errorf(notifierExpectedOne, len(handlers))
	}
}

func TestDiscordFactory_Build_bot_token_creates_handler(t *testing.T) {
	f := &DiscordFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("bot-token-xyz"), 0600); err != nil {
		t.Fatal(err)
	}

	inst := unmarshalDiscordInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  bot_token:
    token_file: `+tokenFile+`
    channel_id: "123456789"`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if len(handlers) != 1 {
		t.Errorf(notifierExpectedOne, len(handlers))
	}
}

func TestDiscordFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &DiscordFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalDiscordInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  webhook_url_file: https://discord.com/api/webhooks/test
when: "`+notifierBadSyntax+`"`)

	_, err := f.Build(inst, log)

	if err == nil {
		t.Error("notifierExpectedCELError")
	}
}

// --- Datadog Factory Tests ---

func TestDatadogFactory_Build_returns_nil_when_instance_notifierDisabled(t *testing.T) {
	f := &DatadogFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalDatadogInstance(t, `name: `+notifierDisabled+`
enabled: false
auth:
  api_key_file: `+notifierKey)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for notifierDisabled instance, got %d", len(handlers))
	}
}

func TestDatadogFactory_Build_creates_handler_when_enabled(t *testing.T) {
	f := &DatadogFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "key")
	if err := os.WriteFile(keyFile, []byte("test-api-key"), 0600); err != nil {
		t.Fatal(err)
	}

	inst := unmarshalDatadogInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  api_key_file: `+keyFile+`
site: datadoghq.com
tags:
  - env:prod`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if len(handlers) != 1 {
		t.Errorf(notifierExpectedOne, len(handlers))
	}
}

func TestDatadogFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &DatadogFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalDatadogInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  api_key_file: `+notifierKey+`
when: "`+notifierBadSyntax+`"`)

	_, err := f.Build(inst, log)

	if err == nil {
		t.Error("notifierExpectedCELError")
	}
}

// --- PagerDuty Factory Tests ---

func TestPagerDutyFactory_Build_returns_nil_when_instance_notifierDisabled(t *testing.T) {
	f := &PagerDutyFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalPagerDutyInstance(t, `name: `+notifierDisabled+`
enabled: false
auth:
  integration_key_file: `+notifierKey)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for notifierDisabled instance, got %d", len(handlers))
	}
}

func TestPagerDutyFactory_Build_creates_handler_when_enabled(t *testing.T) {
	f := &PagerDutyFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "key")
	if err := os.WriteFile(keyFile, []byte("test-integration-key"), 0600); err != nil {
		t.Fatal(err)
	}

	inst := unmarshalPagerDutyInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  integration_key_file: `+keyFile+`
severity: critical`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if len(handlers) != 1 {
		t.Errorf(notifierExpectedOne, len(handlers))
	}
}

func TestPagerDutyFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &PagerDutyFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalPagerDutyInstance(t, `name: `+notifierMain+`
enabled: true
auth:
  integration_key_file: `+notifierKey+`
when: "`+notifierBadSyntax+`"`)

	_, err := f.Build(inst, log)

	if err == nil {
		t.Error("notifierExpectedCELError")
	}
}

// --- Webhook Factory Tests ---

func TestWebhookFactory_Build_returns_nil_when_instance_notifierDisabled(t *testing.T) {
	f := &WebhookFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalWebhookInstance(t, `name: `+notifierDisabled+`
enabled: false
url_file: https://example.com/webhook`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if handlers != nil {
		t.Errorf("expected nil handlers for notifierDisabled instance, got %d", len(handlers))
	}
}

func TestWebhookFactory_Build_creates_handler_when_enabled(t *testing.T) {
	f := &WebhookFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	urlFile := filepath.Join(tmpDir, "url")
	if err := os.WriteFile(urlFile, []byte("https://example.com/webhook"), 0600); err != nil {
		t.Fatal(err)
	}

	inst := unmarshalWebhookInstance(t, `name: `+notifierMain+`
enabled: true
url_file: `+urlFile+`
headers:
  X-Token: secret`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if len(handlers) != 1 {
		t.Errorf(notifierExpectedOne, len(handlers))
	}
}

func TestWebhookFactory_Build_wraps_handler_with_CEL_when_present(t *testing.T) {
	f := &WebhookFactory{}
	log, _ := zap.NewDevelopment()

	tmpDir := t.TempDir()
	urlFile := filepath.Join(tmpDir, "url")
	if err := os.WriteFile(urlFile, []byte("https://example.com/webhook"), 0600); err != nil {
		t.Fatal(err)
	}

	inst := unmarshalWebhookInstance(t, `name: `+notifierMain+`
enabled: true
url_file: `+urlFile+`
when: "event.State == 'failure'"`)

	handlers, err := f.Build(inst, log)

	if err != nil {
		t.Fatalf(notifierUnexpectedError, err)
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 CEL-wrapped handler, got %d", len(handlers))
	}
}

func TestWebhookFactory_Build_returns_error_for_invalid_CEL(t *testing.T) {
	f := &WebhookFactory{}
	log, _ := zap.NewDevelopment()

	inst := unmarshalWebhookInstance(t, `name: `+notifierMain+`
enabled: true
url_file: https://example.com/webhook
when: "`+notifierBadSyntax+`"`)

	_, err := f.Build(inst, log)

	if err == nil {
		t.Error("notifierExpectedCELError")
	}
}
