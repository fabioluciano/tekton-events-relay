package factory

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/accumulator"
	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// BuildOption customizes BuildAll behavior.
type BuildOption func(*buildOptions)

type buildOptions struct {
	accumulatorBuffer accumulator.Buffer
}

// WithAccumulatorBuffer injects a shared-state buffer (valkey/olric) into the
// accumulator instead of the default per-pod in-memory LRU.
func WithAccumulatorBuffer(buf accumulator.Buffer) BuildOption {
	return func(o *buildOptions) { o.accumulatorBuffer = buf }
}

// BuildAll constructs a fully populated Registry from the application config.
// It iterates over all configured SCM and notifier instances, delegates to
// the appropriate factory, and registers the resulting handlers.
// Order: SCM handlers → notifier handlers → accumulator (so the accumulator
// can look up already-registered SCM/notifier providers by name).
func BuildAll(cfg *config.Config, log *zap.Logger, opts ...BuildOption) (*notifier.Registry, error) {
	var options buildOptions
	for _, opt := range opts {
		opt(&options)
	}

	reg := notifier.NewRegistry()

	// SCM providers — registered first so the accumulator can look them up
	if err := buildSCMHandlers(cfg, log, reg); err != nil {
		return nil, err
	}

	// Generic notifiers (Slack, Teams, Discord, PagerDuty, Datadog, Webhook)
	if err := buildNotifierHandlers(cfg, log, reg); err != nil {
		return nil, err
	}

	// Jira issue tracking (top-level integration)
	if err := BuildAndRegister(cfg.Jira, &JiraFactory{}, log, reg); err != nil {
		return nil, err
	}

	// F3 Accumulator — built last so it can find providers already in the registry
	if cfg.Accumulator.Enabled {
		accHandler, err := BuildAccumulator(cfg.Accumulator, reg, options.accumulatorBuffer, log)
		if err != nil {
			return nil, fmt.Errorf("build accumulator: %w", err)
		}
		if accHandler != nil {
			reg.Register(accHandler)
		}
	}

	return reg, nil
}

// buildSCMHandlers iterates SCM instances and builds handlers via factories.
//
//nolint:dupl // Intentional symmetry with buildNotifierHandlers
func buildSCMHandlers(cfg *config.Config, log *zap.Logger, reg *notifier.Registry) error {
	// GitHub
	if err := BuildAndRegister(cfg.SCM.GitHub, &GitHubFactory{}, log, reg); err != nil {
		return err
	}

	// GitLab
	if err := BuildAndRegister(cfg.SCM.GitLab, &GitLabFactory{}, log, reg); err != nil {
		return err
	}

	// Bitbucket
	if err := BuildAndRegister(cfg.SCM.Bitbucket, &BitbucketFactory{}, log, reg); err != nil {
		return err
	}

	// Azure DevOps
	if err := BuildAndRegister(cfg.SCM.Azure, &AzureFactory{}, log, reg); err != nil {
		return err
	}

	// Gitea
	if err := BuildAndRegister(cfg.SCM.Gitea, &GiteaFactory{}, log, reg); err != nil {
		return err
	}

	// SourceHut
	if err := BuildAndRegister(cfg.SCM.SourceHut, &SourceHutFactory{}, log, reg); err != nil {
		return err
	}
	return nil
}

// buildNotifierHandlers iterates notifier instances and builds handlers via factories.
//
//nolint:dupl // Intentional symmetry with buildSCMHandlers
func buildNotifierHandlers(cfg *config.Config, log *zap.Logger, reg *notifier.Registry) error {
	// Slack
	if err := BuildAndRegister(cfg.Notifiers.Slack, &SlackFactory{}, log, reg); err != nil {
		return err
	}

	// Teams
	if err := BuildAndRegister(cfg.Notifiers.Teams, &TeamsFactory{}, log, reg); err != nil {
		return err
	}

	// Discord
	if err := BuildAndRegister(cfg.Notifiers.Discord, &DiscordFactory{}, log, reg); err != nil {
		return err
	}

	// PagerDuty
	if err := BuildAndRegister(cfg.Notifiers.PagerDuty, &PagerDutyFactory{}, log, reg); err != nil {
		return err
	}

	// Datadog
	if err := BuildAndRegister(cfg.Notifiers.Datadog, &DatadogFactory{}, log, reg); err != nil {
		return err
	}

	// Webhook
	if err := BuildAndRegister(cfg.Notifiers.Webhook, &WebhookFactory{}, log, reg); err != nil {
		return err
	}

	// Grafana
	if err := BuildAndRegister(cfg.Notifiers.Grafana, &GrafanaFactory{}, log, reg); err != nil {
		return err
	}

	// Sentry
	if err := BuildAndRegister(cfg.Notifiers.Sentry, &SentryFactory{}, log, reg); err != nil {
		return err
	}

	// Email
	if err := BuildAndRegister(cfg.Notifiers.Email, &EmailFactory{}, log, reg); err != nil {
		return err
	}

	return nil
}
