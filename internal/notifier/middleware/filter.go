package middleware

import (
	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// WrapWithFilter wraps a handler with a filter guard if cfg is non-nil.
func WrapWithFilter(handler notifier.ActionHandler, filterCfg *config.ActionFilterConfig) notifier.ActionHandler {
	if filterCfg == nil {
		return handler
	}
	return notifier.NewFilteredHandler(handler, convertFilterConfig(filterCfg))
}

// convertFilterConfig converts a config.ActionFilterConfig to a notifier.FilterConfig.
func convertFilterConfig(cfg *config.ActionFilterConfig) *notifier.FilterConfig {
	return &notifier.FilterConfig{
		Tasks: notifier.FilterList{
			Allow: cfg.Tasks.Allow,
			Deny:  cfg.Tasks.Deny,
		},
		Pipelines: notifier.FilterList{
			Allow: cfg.Pipelines.Allow,
			Deny:  cfg.Pipelines.Deny,
		},
		CustomRuns: notifier.FilterList{
			Allow: cfg.CustomRuns.Allow,
			Deny:  cfg.CustomRuns.Deny,
		},
		EventListeners: notifier.FilterList{
			Allow: cfg.EventListeners.Allow,
			Deny:  cfg.EventListeners.Deny,
		},
	}
}
