package middleware

import (
	"context"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// contextPerTaskHandler rewrites the status context of TaskRun events to
// "<context>/<task>", producing one independent commit status per task so
// branch protection rules can require specific checks.
type contextPerTaskHandler struct {
	inner notifier.ActionHandler
}

// WrapWithContextPerTask decorates commit status handlers when the action
// sets context_per_task: true. Non-TaskRun events pass through untouched.
func WrapWithContextPerTask(h notifier.ActionHandler, enabled bool) notifier.ActionHandler {
	if !enabled {
		return h
	}
	return &contextPerTaskHandler{inner: h}
}

func (c *contextPerTaskHandler) Name() string              { return c.inner.Name() }
func (c *contextPerTaskHandler) Type() notifier.ActionType { return c.inner.Type() }

func (c *contextPerTaskHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Resource == domain.ResourceTaskRun {
		task := e.PipelineTaskName
		if task == "" {
			task = e.TaskName
		}
		if task == "" {
			task = e.RunName
		}
		if e.Context != "" {
			e.Context = e.Context + "/" + task
		} else {
			e.Context = task
		}
	}
	return c.inner.Handle(ctx, e)
}
