package factory

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/accumulator"
	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/store"
)

// currentDedupeStore is the notifier dedupe store set by BuildAll and read
// by buildActionsWithMiddleware so SCM actions can wrap handlers with a
// dedupe guard without threading the value through every factory struct.
// There is only one caller of BuildAll at a time (the app's main/reload
// path) so a package-level variable is safe.
var currentDedupeStore notifier.NotifierDedupeStore

// BuildOption customizes BuildAll behavior.
type BuildOption func(*buildOptions)

type buildOptions struct {
	accumulatorBuffer accumulator.Buffer
	dedupeStore       store.DedupeStore
}

// WithAccumulatorBuffer injects a shared-state buffer (valkey/olric) into the
// accumulator instead of the default per-pod in-memory LRU.
func WithAccumulatorBuffer(buf accumulator.Buffer) BuildOption {
	return func(o *buildOptions) { o.accumulatorBuffer = buf }
}

// WithDedupeStore injects the store's DedupeStore so notifier and SCM
// handlers can be wrapped with a notification-specific dedupe guard.
func WithDedupeStore(s store.DedupeStore) BuildOption {
	return func(o *buildOptions) { o.dedupeStore = s }
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

	// Make the notifier dedupe store available to buildActionsWithMiddleware.
	if options.dedupeStore != nil {
		currentDedupeStore = notifier.NewNotifierDedupeStore(options.dedupeStore)
	} else {
		currentDedupeStore = nil
	}

	reg := notifier.NewRegistry()

	// SCM providers — registered first so the accumulator can look them up
	if err := buildSCMHandlers(cfg, log, reg); err != nil {
		currentDedupeStore = nil
		return nil, err
	}

	// Generic notifiers (Slack, Teams, Discord, PagerDuty, Datadog, Webhook)
	if err := buildNotifierHandlers(cfg, log, reg); err != nil {
		currentDedupeStore = nil
		return nil, err
	}

	// Jira issue tracking (top-level integration)
	if err := buildJiraHandlers(cfg, log, reg); err != nil {
		currentDedupeStore = nil
		return nil, err
	}

	// F3 Accumulator — built last so it can find providers already in the registry
	if cfg.Accumulator.Enabled {
		accHandler, err := BuildAccumulator(cfg.Accumulator, reg, options.accumulatorBuffer, log)
		if err != nil {
			currentDedupeStore = nil
			return nil, fmt.Errorf("build accumulator: %w", err)
		}
		if accHandler != nil {
			reg.Register(accHandler)
		}
	}

	currentDedupeStore = nil
	return reg, nil
}

// buildJiraHandlers builds Jira handlers with optional dedupe wrapping.
func buildJiraHandlers(cfg *config.Config, log *zap.Logger, reg *notifier.Registry) error {
	for i := range cfg.Jira {
		inst := cfg.Jira[i]
		if !inst.Enabled {
			continue
		}
		handlers, err := (&JiraFactory{}).Build(inst, log)
		if err != nil {
			return err
		}
		for _, h := range handlers {
			if inst.Dedupe && currentDedupeStore != nil {
				h = middleware.WrapWithDedupe(h, currentDedupeStore, log)
			}
			reg.Register(h)
		}
	}
	return nil
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

// buildAndRegisterWithDedupe is like BuildAndRegister but wraps each handler
// with a dedupe guard when the instance config has Dedupe enabled.
func buildAndRegisterWithDedupe[C any](
	instances []C,
	f HandlerFactory[C],
	log *zap.Logger,
	reg *notifier.Registry,
	getDedupe func(inst C) bool,
) error {
	for _, inst := range instances {
		handlers, err := f.Build(inst, log)
		if err != nil {
			return err
		}
		for _, h := range handlers {
			if getDedupe(inst) && currentDedupeStore != nil {
				h = middleware.WrapWithDedupe(h, currentDedupeStore, log)
			}
			reg.Register(h)
		}
	}
	return nil
}

// buildNotifierHandlers iterates notifier instances and builds handlers via factories.
// Each handler is optionally wrapped with dedupe when the instance has Dedupe enabled.
func buildNotifierHandlers(cfg *config.Config, log *zap.Logger, reg *notifier.Registry) error {
	// Slack
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Slack, &SlackFactory{}, log, reg,
		func(inst config.SlackInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Teams
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Teams, &TeamsFactory{}, log, reg,
		func(inst config.TeamsInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Discord
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Discord, &DiscordFactory{}, log, reg,
		func(inst config.DiscordInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// PagerDuty
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.PagerDuty, &PagerDutyFactory{}, log, reg,
		func(inst config.PagerDutyInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Datadog
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Datadog, &DatadogFactory{}, log, reg,
		func(inst config.DatadogInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Webhook
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Webhook, &WebhookFactory{}, log, reg,
		func(inst config.WebhookInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Grafana
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Grafana, &GrafanaFactory{}, log, reg,
		func(inst config.GrafanaInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Sentry
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Sentry, &SentryFactory{}, log, reg,
		func(inst config.SentryInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Email
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Email, &EmailFactory{}, log, reg,
		func(inst config.EmailInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Mattermost
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Mattermost, &MattermostFactory{}, log, reg,
		func(inst config.MattermostInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Telegram
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Telegram, &TelegramFactory{}, log, reg,
		func(inst config.TelegramInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Incident.io
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.IncidentIO, &IncidentIOFactory{}, log, reg,
		func(inst config.IncidentIOInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// New Relic
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.NewRelic, &NewRelicFactory{}, log, reg,
		func(inst config.NewRelicInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Opsgenie
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Opsgenie, &OpsgenieFactory{}, log, reg,
		func(inst config.OpsgenieInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Honeycomb
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Honeycomb, &HoneycombFactory{}, log, reg,
		func(inst config.HoneycombInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Kafka
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.Kafka, &KafkaFactory{}, log, reg,
		func(inst config.KafkaInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// NATS
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.NATS, &NATSFactory{}, log, reg,
		func(inst config.NATSInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// RabbitMQ
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.RabbitMQ, &RabbitMQFactory{}, log, reg,
		func(inst config.RabbitMQInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	// Redis Pub/Sub
	if err := buildAndRegisterWithDedupe(cfg.Notifiers.RedisPubSub, &RedisPubSubFactory{}, log, reg,
		func(inst config.RedisPubSubInstance) bool { return inst.Dedupe }); err != nil {
		return err
	}

	return nil
}
