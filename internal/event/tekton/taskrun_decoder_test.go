package tekton

import (
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

const (
	typeTaskSuccessful = "dev.tekton.event.taskrun.successful.v1"
	typeTaskFailed     = "dev.tekton.event.taskrun.failed.v1"

	testEventIDTaskrun = "evt-1"
)

func TestTaskRunDecoder_Name(t *testing.T) {
	d := NewTaskRunDecoder()
	if d.Name() != decoderNameTaskRun {
		t.Errorf("Name() = %q, want %s", d.Name(), decoderNameTaskRun)
	}
}

func TestTaskRunDecoder_CanHandle(t *testing.T) {
	d := NewTaskRunDecoder()
	tests := []struct {
		eventType string
		want      bool
	}{
		{"dev.tekton.event.taskrun.started.v1", true},
		{"dev.tekton.event.taskrun.successful.v1", true}, //nolint:goconst
		{"dev.tekton.event.taskrun.failed.v1", true},
		{"dev.tekton.event.pipelinerun.started.v1", false},
		{"dev.tekton.event.customrun.started.v1", false},
		{"io.example.foreign.v1.completed", false},
	}
	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			if got := d.CanHandle(tt.eventType); got != tt.want {
				t.Errorf("CanHandle(%q) = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestTaskRunDecoder_Decode_Success(t *testing.T) {
	payload := `{
  "taskRun": {
    "metadata": {
      "name": "test-task-123",
      "namespace": "default",
      "uid": "abc-def-ghi",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "gitlab",
        "tekton.dev/tekton-events-relay.scm.repo-owner": "myorg",
        "tekton.dev/tekton-events-relay.scm.repo-name": "myrepo",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "def456",
        "tekton.dev/tekton-events-relay.scm.api-base-url": "https://gitlab.example.com"
      }
    },
    "status": {
      "startTime": "2023-01-15T10:30:00Z",
      "completionTime": "2023-01-15T10:35:00Z",
      "conditions": [{"type":"Succeeded","status":"True","reason":"Succeeded","message":"Task completed successfully"}]
    }
  }
}`

	d := NewTaskRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:     "evt-task-1",
		Type:   typeTaskSuccessful,
		Source: "tekton-controller",
		Data:   []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if env.Report.Resource != domain.ResourceTaskRun {
		t.Errorf("Resource = %q, want taskrun", env.Report.Resource)
	}
	if env.Report.Provider != "gitlab" {
		t.Errorf("Provider = %q, want gitlab", env.Report.Provider)
	}
	if env.Report.State != domain.StateSuccess {
		t.Errorf("State = %q, want success", env.Report.State)
	}
	if env.Report.RunName != "test-task-123" {
		t.Errorf("RunName = %q", env.Report.RunName)
	}
	if env.Report.Namespace != "default" {
		t.Errorf("Namespace = %q", env.Report.Namespace)
	}
	if env.Report.APIBaseURL != "https://gitlab.example.com" {
		t.Errorf("APIBaseURL = %q", env.Report.APIBaseURL)
	}
	if env.CloudEventID != "evt-task-1" {
		t.Errorf("CloudEventID = %q", env.CloudEventID)
	}
	if env.Source != "tekton-controller" {
		t.Errorf("Source = %q", env.Source)
	}
}

func TestTaskRunDecoder_Decode_AllStates(t *testing.T) {
	tests := []struct {
		eventType string
		want      domain.State
		desc      string
	}{
		{"dev.tekton.event.taskrun.queued.v1", domain.StatePending, "Queued"},
		{"dev.tekton.event.taskrun.started.v1", domain.StatePending, "Started"},
		{"dev.tekton.event.taskrun.running.v1", domain.StateRunning, "Running"},
		{"dev.tekton.event.taskrun.successful.v1", domain.StateSuccess, "Succeeded"},
		{"dev.tekton.event.taskrun.failed.v1", domain.StateFailure, "Failed"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			payload := `{
  "taskRun": {
    "metadata": {
      "name": "task-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.repo-owner": "owner",
        "tekton.dev/tekton-events-relay.scm.repo-name": "repo",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "sha123"
      }
    },
    "status": {}
  }
}`
			d := NewTaskRunDecoder()
			env, err := d.Decode(event.RawEvent{
				ID:   testEventIDTaskrun,
				Type: tt.eventType,
				Data: []byte(payload),
			})
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}
			if env.Report.State != tt.want {
				t.Errorf("State = %q, want %q", env.Report.State, tt.want)
			}
			if env.Report.Description != tt.desc {
				t.Errorf("Description = %q, want %q", env.Report.Description, tt.desc)
			}
		})
	}
}

func TestTaskRunDecoder_Decode_InvalidEventType(t *testing.T) {
	d := NewTaskRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1", //nolint:goconst
		Type: "dev.tekton.event.pipelinerun.successful.v1",
		Data: []byte(`{"taskRun":{}}`),
	})
	if err == nil {
		t.Fatal("expected error for non-taskrun event type")
	}
	if err.Error() != "not a taskrun event: dev.tekton.event.pipelinerun.successful.v1" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTaskRunDecoder_Decode_InvalidJSON(t *testing.T) {
	d := NewTaskRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeTaskSuccessful,
		Data: []byte(`{invalid json`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestTaskRunDecoder_Decode_EmptyPayload(t *testing.T) {
	d := NewTaskRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeTaskSuccessful,
		Data: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
	if err.Error() != "payload has no taskRun" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTaskRunDecoder_Decode_MissingProvider(t *testing.T) {
	payload := `{
  "taskRun": {
    "metadata": {
      "name": "task-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123"
      }
    },
    "status": {}
  }
}`
	d := NewTaskRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeTaskSuccessful,
		Data: []byte(payload),
	})
	if err == nil {
		t.Fatal("expected error for missing provider annotation")
	}
	if err.Error() != "missing annotation tekton.dev/tekton-events-relay.scm.provider on ci/task-1" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTaskRunDecoder_Decode_MissingCommitSHA(t *testing.T) {
	payload := `{
  "taskRun": {
    "metadata": {
      "name": "task-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.repo-owner": "test",
        "tekton.dev/tekton-events-relay.scm.repo-name": "repo"
      }
    },
    "status": {}
  }
}`
	d := NewTaskRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeTaskSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("decode should succeed without commit SHA (for issue/discussion triggers): %v", err)
	}
	if env.Report.CommitSHA != "" {
		t.Errorf("expected empty SHA, got %q", env.Report.CommitSHA)
	}
}

func TestTaskRunDecoder_Decode_ContextFallback(t *testing.T) {
	payload := `{
  "taskRun": {
    "metadata": {
      "name": "my-task-456",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.repo-owner": "owner",
        "tekton.dev/tekton-events-relay.scm.repo-name": "repo",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "sha123"
      }
    },
    "status": {}
  }
}`
	d := NewTaskRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeTaskSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if env.Report.Context != "tekton/task/my-task-456" {
		t.Errorf("Context = %q, want tekton/task/my-task-456", env.Report.Context)
	}
}

func TestTaskRunDecoder_Decode_WithTimestamps(t *testing.T) {
	payload := `{
  "taskRun": {
    "metadata": {
      "name": "task-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123"
      }
    },
    "status": {
      "startTime": "2023-01-15T10:00:00Z",
      "completionTime": "2023-01-15T10:05:30Z"
    }
  }
}`
	d := NewTaskRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeTaskSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	expectedStart, _ := time.Parse(time.RFC3339, "2023-01-15T10:00:00Z")
	expectedEnd, _ := time.Parse(time.RFC3339, "2023-01-15T10:05:30Z")

	if !env.Report.StartedAt.Equal(expectedStart) {
		t.Errorf("StartedAt = %v, want %v", env.Report.StartedAt, expectedStart)
	}
	if !env.Report.FinishedAt.Equal(expectedEnd) {
		t.Errorf("FinishedAt = %v, want %v", env.Report.FinishedAt, expectedEnd)
	}
}

func TestTaskRunDecoder_Decode_WithAllAnnotations(t *testing.T) {
	payload := `{
  "taskRun": {
    "metadata": {
      "name": "task-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "azure",
        "tekton.dev/tekton-events-relay.scm.repo-owner": "owner1",
        "tekton.dev/tekton-events-relay.scm.repo-name": "repo1",
        "tekton.dev/tekton-events-relay.scm.repo-id": "12345",
        "tekton.dev/tekton-events-relay.scm.repo-workspace": "ws1",
        "tekton.dev/tekton-events-relay.scm.repo-project": "proj1",
        "tekton.dev/tekton-events-relay.scm.repo-org": "org1",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123",
        "tekton.dev/tekton-events-relay.scm.api-base-url": "https://dev.azure.com",
        "tekton.dev/tekton-events-relay.scm.context": "custom/context",
        "tekton.dev/tekton-events-relay.scm.issue-number": "42",
        "tekton.dev/tekton-events-relay.scm.pr-number": "123",
        "tekton.dev/tekton-events-relay.scm.discussion-number": "99"
      },
      "labels": {
        "tekton.dev/task": "build-task",
        "tekton.dev/pipeline": "main-pipeline"
      }
    },
    "status": {}
  }
}`
	d := NewTaskRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeTaskSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	repo := env.Report.Repo
	if repo.Owner != "owner1" {
		t.Errorf("Repo.Owner = %q", repo.Owner)
	}
	if repo.Name != "repo1" {
		t.Errorf("Repo.Name = %q", repo.Name)
	}
	if repo.ID != "12345" {
		t.Errorf("Repo.ID = %q", repo.ID)
	}
	if repo.Workspace != "ws1" {
		t.Errorf("Repo.Workspace = %q", repo.Workspace)
	}
	if repo.Project != "proj1" {
		t.Errorf("Repo.Project = %q", repo.Project)
	}
	if repo.Org != "org1" {
		t.Errorf("Repo.Org = %q", repo.Org)
	}
	if env.Report.APIBaseURL != "https://dev.azure.com" {
		t.Errorf("APIBaseURL = %q", env.Report.APIBaseURL)
	}
	if env.Report.Context != "custom/context" {
		t.Errorf("Context = %q", env.Report.Context)
	}
	if env.Report.TaskName != "build-task" {
		t.Errorf("TaskName = %q", env.Report.TaskName)
	}
	if env.Report.PipelineName != "main-pipeline" {
		t.Errorf("PipelineName = %q", env.Report.PipelineName)
	}
	if env.Report.IssueNumber == nil || *env.Report.IssueNumber != 42 {
		t.Errorf("IssueNumber = %v, want 42", env.Report.IssueNumber)
	}
	if env.Report.PRNumber == nil || *env.Report.PRNumber != 123 {
		t.Errorf("PRNumber = %v, want 123", env.Report.PRNumber)
	}
	if env.Report.DiscussionNumber == nil || *env.Report.DiscussionNumber != 99 {
		t.Errorf("DiscussionNumber = %v, want 99", env.Report.DiscussionNumber)
	}
}

func TestTaskRunDecoder_Decode_DescriptionFromCondition(t *testing.T) {
	payload := `{
  "taskRun": {
    "metadata": {
      "name": "task-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123"
      }
    },
    "status": {
      "conditions": [
        {"type":"Other","status":"True","reason":"","message":"Ignored"},
        {"type":"Succeeded","status":"False","reason":"Failed","message":"Tests failed in unit-test"}
      ]
    }
  }
}`
	d := NewTaskRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeTaskFailed,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if env.Report.Description != "Tests failed in unit-test" {
		t.Errorf("Description = %q", env.Report.Description)
	}
}

func TestTaskRunDecoder_Decode_DescriptionTruncation(t *testing.T) {
	longMsg := "This is a very long message that exceeds 140 characters and should be truncated with ellipsis at the end to ensure it fits properly in status displays and notifications"
	payload := `{
  "taskRun": {
    "metadata": {
      "name": "task-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123"
      }
    },
    "status": {
      "conditions": [
        {"type":"Succeeded","status":"True","reason":"Succeeded","message":"` + longMsg + `"}
      ]
    }
  }
}`
	d := NewTaskRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeTaskSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if len(env.Report.Description) != 140 {
		t.Errorf("Description length = %d, want 140", len(env.Report.Description))
	}
	if env.Report.Description[137:] != "..." {
		t.Errorf("Description should end with ..., got: %q", env.Report.Description[137:])
	}
}
