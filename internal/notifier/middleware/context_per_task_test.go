package middleware

import (
	"context"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const testContext = "tekton/ci"

type captureHandler struct{ last domain.Event }

func (c *captureHandler) Name() string              { return "capture" }
func (c *captureHandler) Type() notifier.ActionType { return notifier.ActionCommitStatus }
func (c *captureHandler) Handle(_ context.Context, e domain.Event) error {
	c.last = e
	return nil
}

func TestContextPerTask_RewritesTaskRunContext(t *testing.T) {
	inner := &captureHandler{}
	h := WrapWithContextPerTask(inner, true)

	_ = h.Handle(context.Background(), domain.Event{
		Resource: domain.ResourceTaskRun, Context: testContext, PipelineTaskName: "build",
	})
	if inner.last.Context != testContext+"/build" {
		t.Errorf("context = %q, want testContext/build", inner.last.Context)
	}

	_ = h.Handle(context.Background(), domain.Event{
		Resource: domain.ResourcePipelineRun, Context: testContext,
	})
	if inner.last.Context != testContext {
		t.Errorf("pipelinerun context = %q, want unchanged", inner.last.Context)
	}
}

func TestContextPerTask_DisabledPassthrough(t *testing.T) {
	inner := &captureHandler{}
	h := WrapWithContextPerTask(inner, false)
	_ = h.Handle(context.Background(), domain.Event{
		Resource: domain.ResourceTaskRun, Context: testContext, TaskName: "build",
	})
	if inner.last.Context != testContext {
		t.Errorf("context = %q, want unchanged when disabled", inner.last.Context)
	}
}
