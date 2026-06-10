package cel

import (
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func fieldsEvent() domain.Event {
	return domain.Event{
		Provider:            "github",
		Resource:            domain.ResourcePipelineRun,
		APIBaseURL:          "https://ghe.example.com",
		RunName:             "run-1",
		RunID:               "uid-1",
		Namespace:           "prod",
		TaskName:            "build",
		PipelineName:        "ci",
		PipelineTaskName:    "compile",
		TriggerName:         "on-push",
		TaskDisplayName:     "Build",
		PipelineDisplayName: "CI",
		TaskCount:           3,
		State:               domain.StateSuccess,
		TargetURL:           "https://tekton.example.com/run-1",
		Results: []domain.Result{
			{Name: "IMAGE_DIGEST", Value: "sha256:abc"},
		},
		StartedAt:  time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 1, 1, 10, 5, 0, 0, time.UTC),
	}
}

func TestEval_NewActivationFields(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"task_count", `event.TaskCount > 2`},
		{"target_url", `event.TargetURL.startsWith("https://tekton")`},
		{"pipeline_task_name", `event.PipelineTaskName == "compile"`},
		{"trigger_name", `event.TriggerName == "on-push"`},
		{"api_base_url", `event.APIBaseURL.contains("ghe")`},
		{"display_names", `event.TaskDisplayName == "Build" && event.PipelineDisplayName == "CI"`},
		{"results_map", `event.Results["IMAGE_DIGEST"].startsWith("sha256:")`},
		{"timestamps", `event.FinishedAt > event.StartedAt`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := Compile(tc.expr)
			if err != nil {
				t.Fatalf("compile %q: %v", tc.expr, err)
			}
			ok, err := prog.Eval(fieldsEvent())
			if err != nil {
				t.Fatalf("eval %q: %v", tc.expr, err)
			}
			if !ok {
				t.Errorf("expression %q = false, want true", tc.expr)
			}
		})
	}
}
