package pipeline

import (
	"context"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// EventFilter drops events based on resource type and unknown state configuration.
type EventFilter struct {
	BaseHandler
	AllowTaskRun       bool
	AllowPipelineRun   bool
	AllowCustomRun     bool
	AllowEventListener bool
	// IgnoreUnknown ignores dev.tekton.event.*.unknown.v1, which is emitted on
	// every Condition change during execution (generates noise in the PR).
	IgnoreUnknown bool
}

// NewEventFilter creates a new EventFilter with the given configuration.
func NewEventFilter(allowTask, allowPipeline, allowCustom, allowEventListener, ignoreUnknown bool) *EventFilter {
	return &EventFilter{
		AllowTaskRun:       allowTask,
		AllowPipelineRun:   allowPipeline,
		AllowCustomRun:     allowCustom,
		AllowEventListener: allowEventListener,
		IgnoreUnknown:      ignoreUnknown,
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
	return f.Next(ctx, env)
}
