package notifier

import (
	"context"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// FilterConfig configures action-level filtering with allow/deny lists.
// Mirrors config.ActionFilterConfig to avoid a dependency on the config package.
type FilterConfig struct {
	Tasks          FilterList `yaml:"tasks,omitempty"`
	Pipelines      FilterList `yaml:"pipelines,omitempty"`
	CustomRuns     FilterList `yaml:"custom_runs,omitempty"`
	EventListeners FilterList `yaml:"event_listeners,omitempty"`
}

// FilterList defines allow and deny lists for filtering.
type FilterList struct {
	Allow []string `yaml:"allow,omitempty"`
	Deny  []string `yaml:"deny,omitempty"`
}

// FilteredHandler wraps an ActionHandler with action-level filtering.
// Supports allow/deny lists per resource type (tasks, pipelines, custom_runs, event_listeners).
// Filtering is case-insensitive. If cfg is nil, all events pass through.
type FilteredHandler struct {
	inner ActionHandler
	cfg   *FilterConfig

	// Pre-built maps for O(1) lookup (lowercase keys)
	tasksAllow          map[string]struct{}
	tasksDeny           map[string]struct{}
	pipelinesAllow      map[string]struct{}
	pipelinesDeny       map[string]struct{}
	customRunsAllow     map[string]struct{}
	customRunsDeny      map[string]struct{}
	eventListenersAllow map[string]struct{}
	eventListenersDeny  map[string]struct{}
}

// NewFilteredHandler creates a filtered wrapper around an ActionHandler.
// If cfg is nil, the handler passes all events through (no filtering).
func NewFilteredHandler(inner ActionHandler, cfg *FilterConfig) *FilteredHandler {
	h := &FilteredHandler{
		inner: inner,
		cfg:   cfg,
	}

	if cfg != nil {
		h.tasksAllow = buildMap(cfg.Tasks.Allow)
		h.tasksDeny = buildMap(cfg.Tasks.Deny)
		h.pipelinesAllow = buildMap(cfg.Pipelines.Allow)
		h.pipelinesDeny = buildMap(cfg.Pipelines.Deny)
		h.customRunsAllow = buildMap(cfg.CustomRuns.Allow)
		h.customRunsDeny = buildMap(cfg.CustomRuns.Deny)
		h.eventListenersAllow = buildMap(cfg.EventListeners.Allow)
		h.eventListenersDeny = buildMap(cfg.EventListeners.Deny)
	}

	return h
}

// buildMap converts a slice of strings to a map with lowercase keys for case-insensitive lookup.
func buildMap(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(items))
	for _, item := range items {
		m[strings.ToLower(item)] = struct{}{}
	}
	return m
}

// Name returns the inner handler name.
func (f *FilteredHandler) Name() string {
	return f.inner.Name()
}

// Type returns the inner handler type.
func (f *FilteredHandler) Type() ActionType {
	return f.inner.Type()
}

// Handle applies filtering logic before delegating to the inner handler.
// Returns nil (drops event) if the filter rejects it.
// Delegates to inner.Handle if the event passes the filter.
func (f *FilteredHandler) Handle(ctx context.Context, e domain.Event) error {
	// No filter config: pass all events
	if f.cfg == nil {
		return f.inner.Handle(ctx, e)
	}

	// Select the appropriate filter lists based on resource type
	var allowMap, denyMap map[string]struct{}
	var name string

	switch e.Resource {
	case domain.ResourceTaskRun:
		allowMap = f.tasksAllow
		denyMap = f.tasksDeny
		name = e.TaskName
	case domain.ResourcePipelineRun:
		allowMap = f.pipelinesAllow
		denyMap = f.pipelinesDeny
		name = e.PipelineName
	case domain.ResourceCustomRun:
		allowMap = f.customRunsAllow
		denyMap = f.customRunsDeny
		name = e.TaskName // CustomRun uses TaskName field
	case domain.ResourceEventListener:
		allowMap = f.eventListenersAllow
		denyMap = f.eventListenersDeny
		name = e.EventListenerName
	default:
		// Unknown resource type: pass through
		return f.inner.Handle(ctx, e)
	}

	// Empty name: cannot filter, pass through
	if name == "" {
		return f.inner.Handle(ctx, e)
	}

	nameLower := strings.ToLower(name)

	// Check deny list first (deny wins)
	if denyMap != nil {
		if _, denied := denyMap[nameLower]; denied {
			return nil // Drop event
		}
	}

	// Check allow list (if present and non-empty)
	if len(allowMap) > 0 {
		if _, allowed := allowMap[nameLower]; !allowed {
			return nil // Drop event (not in allow list)
		}
	}

	// Event passed filter: delegate to inner handler
	return f.inner.Handle(ctx, e)
}
