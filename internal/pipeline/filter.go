package pipeline

import (
	"context"
	"path"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// EventFilter drops events based on resource type, unknown state, and namespace
// configuration. Wildcard patterns in AllowNamespaces and DenyNamespaces support
// path.Match syntax (e.g. "*.production", "staging-*").
type EventFilter struct {
	BaseHandler
	AllowTaskRun       bool
	AllowPipelineRun   bool
	AllowCustomRun     bool
	AllowEventListener bool
	// IgnoreUnknown ignores dev.tekton.event.*.unknown.v1, which is emitted on
	// every Condition change during execution (generates noise in the PR).
	IgnoreUnknown bool
	// AllowNamespaces is a list of namespace patterns (path.Match syntax).
	// When non-empty, only events whose namespace matches at least one pattern
	// are processed. Empty means all namespaces are allowed.
	AllowNamespaces []string
	// DenyNamespaces is a list of namespace patterns (path.Match syntax).
	// When non-empty, events whose namespace matches any pattern are dropped.
	// Deny wins over allow.
	DenyNamespaces []string
}

// NewEventFilter creates a new EventFilter with the given configuration.
func NewEventFilter(allowTask, allowPipeline, allowCustom, allowEventListener, ignoreUnknown bool, allowNamespaces, denyNamespaces []string) *EventFilter {
	return &EventFilter{
		AllowTaskRun:       allowTask,
		AllowPipelineRun:   allowPipeline,
		AllowCustomRun:     allowCustom,
		AllowEventListener: allowEventListener,
		IgnoreUnknown:      ignoreUnknown,
		AllowNamespaces:    allowNamespaces,
		DenyNamespaces:     denyNamespaces,
	}
}

// Handle filters events based on the configured rules.
// Returns nil (drops the event) if it should be filtered out.
func (f *EventFilter) Handle(ctx context.Context, env *event.Envelope) error {
	if f.IgnoreUnknown && strings.HasSuffix(env.CloudEventType, ".unknown.v1") {
		return nil // drop silently
	}
	switch env.Report.Resource {
	case domain.ResourceTaskRun:
		if !f.AllowTaskRun {
			return nil
		}
	case domain.ResourcePipelineRun:
		if !f.AllowPipelineRun {
			return nil
		}
	case domain.ResourceCustomRun:
		if !f.AllowCustomRun {
			return nil
		}
	case domain.ResourceEventListener:
		if !f.AllowEventListener {
			return nil
		}
	default:
		if f.IgnoreUnknown {
			return nil
		}
	}
	if !f.namespaceAllowed(env.Report.Namespace) {
		return nil
	}
	return f.Next(ctx, env)
}

// matchNamespace reports whether ns matches a pattern from patterns using
// path.Match. Patterns starting with "!" are treated as negations: a match
// returns false (unless overridden by a later non-negated pattern). Returns
// false if no pattern matches.
func matchNamespace(ns string, patterns []string) bool {
	matched := false
	for _, p := range patterns {
		negate := strings.HasPrefix(p, "!")
		if negate {
			p = p[1:]
		}
		ok, err := path.Match(p, ns)
		if err != nil || !ok {
			continue
		}
		matched = !negate
	}
	return matched
}

// namespaceAllowed checks whether the event namespace passes the filter rules.
// Deny patterns are checked first (deny wins). If no deny matches, allow
// patterns are checked. Empty allow list permits all namespaces.
func (f *EventFilter) namespaceAllowed(ns string) bool {
	// Deny wins: if namespace matches any deny pattern, drop it.
	if len(f.DenyNamespaces) > 0 && matchNamespace(ns, f.DenyNamespaces) {
		return false
	}
	// If allow list is empty, all namespaces are permitted.
	if len(f.AllowNamespaces) == 0 {
		return true
	}
	// Allow: namespace must match at least one allow pattern.
	return matchNamespace(ns, f.AllowNamespaces)
}
