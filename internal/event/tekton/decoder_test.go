package tekton

import (
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

const (
	testEventID            = "evt-1"
	typePipelineSuccessful = "dev.tekton.event.pipelinerun.successful.v1"
	typePipelineFailed     = "dev.tekton.event.pipelinerun.failed.v1"
	typeTaskSuccessful     = "dev.tekton.event.taskrun.successful.v1"
	testNamespaceCi        = "ci"
	testProviderGithub     = "github"
	testRepoOwner          = "owner"
	testRepoName           = "repo"
	testCommitSHA          = "sha123"

	samplePayload = `{
  "pipelineRun": {
    "metadata": {
      "name": "build-pr-abc",
      "namespace": "` + testNamespaceCi + `",
      "labels": {"scm.provider": "` + testProviderGithub + `"},
      "annotations": {
        "scm.repo-owner": "fabio",
        "scm.repo-name": "meu-repo",
        "scm.commit-sha": "abc123",
        "scm.context": "tekton/build"
      }
    },
    "status": {
      "conditions": [{"type":"Succeeded","status":"True","reason":"Succeeded","message":"All tasks passed"}]
    }
  }
}`
)

func TestDecode_PipelineRunSuccess(t *testing.T) {
	d := New()
	env, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: typePipelineSuccessful,
		Data: []byte(samplePayload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if env.Report.Provider != testProviderGithub {
		t.Errorf("Provider = %q", env.Report.Provider)
	}
	if env.Report.State != domain.StateSuccess {
		t.Errorf("State = %q, want success", env.Report.State)
	}
	if env.Report.Resource != domain.ResourcePipelineRun {
		t.Errorf("Resource = %q", env.Report.Resource)
	}
}

func TestCanHandle(t *testing.T) {
	d := New()
	if !d.CanHandle("dev.tekton.event.pipelinerun.started.v1") {
		t.Error("should handle tekton events")
	}
	if d.CanHandle("io.example.foreign.v1.completed") {
		t.Error("should not handle foreign events")
	}
}

func TestMapState(t *testing.T) {
	cases := map[string]domain.State{
		"dev.tekton.event.pipelinerun.queued.v1":  domain.StatePending,
		"dev.tekton.event.pipelinerun.started.v1": domain.StatePending,
		"dev.tekton.event.pipelinerun.running.v1": domain.StateRunning,
		typePipelineSuccessful:                    domain.StateSuccess,
		typePipelineFailed:                        domain.StateFailure,
	}
	for evt, want := range cases {
		if got := MapState(evt); got != want {
			t.Errorf("MapState(%q) = %q, want %q", evt, got, want)
		}
	}
}

func TestDecode_TaskRunSuccess(t *testing.T) {
	payload := `{
  "taskRun": {
    "metadata": {
      "name": "test-task-123",
      "namespace": "default",
      "uid": "abc-def-ghi",
      "labels": {"scm.provider": "gitlab"},
      "annotations": {
        "scm.repo-owner": "myorg",
        "scm.repo-name": "myrepo",
        "scm.commit-sha": "def456",
        "scm.api-base-url": "https://gitlab.example.com"
      }
    },
    "status": {
      "startTime": "2023-01-15T10:30:00Z",
      "completionTime": "2023-01-15T10:35:00Z",
      "conditions": [{"type":"Succeeded","status":"True","reason":"Succeeded","message":"Task completed successfully"}]
    }
  }
}`

	d := New()
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

func TestDecode_AllTaskRunStates(t *testing.T) {
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
      "namespace": "` + testNamespaceCi + `",
      "labels": {"scm.provider": "` + testProviderGithub + `"},
      "annotations": {
        "scm.repo-owner": "` + testRepoOwner + `",
        "scm.repo-name": "` + testRepoName + `",
        "scm.commit-sha": "` + testCommitSHA + `"
      }
    },
    "status": {}
  }
}`
			d := New()
			env, err := d.Decode(event.RawEvent{
				ID:   testEventID,
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

func TestDecode_AllPipelineRunStates(t *testing.T) {
	tests := []struct {
		eventType string
		want      domain.State
	}{
		{"dev.tekton.event.pipelinerun.queued.v1", domain.StatePending},
		{"dev.tekton.event.pipelinerun.started.v1", domain.StatePending},
		{"dev.tekton.event.pipelinerun.running.v1", domain.StateRunning},
		{"dev.tekton.event.pipelinerun.unknown.v1", domain.StateRunning},
		{typePipelineSuccessful, domain.StateSuccess},
		{typePipelineFailed, domain.StateFailure},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "pipeline-1",
      "namespace": "` + testNamespaceCi + `",
      "labels": {"scm.provider": "` + testProviderGithub + `"},
      "annotations": {
        "scm.repo-owner": "` + testRepoOwner + `",
        "scm.repo-name": "` + testRepoName + `",
        "scm.commit-sha": "` + testCommitSHA + `"
      }
    },
    "status": {}
  }
}`
			d := New()
			env, err := d.Decode(event.RawEvent{
				ID:   testEventID,
				Type: tt.eventType,
				Data: []byte(payload),
			})
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}
			if env.Report.State != tt.want {
				t.Errorf("State = %q, want %q", env.Report.State, tt.want)
			}
		})
	}
}

func TestDecode_InvalidEventType(t *testing.T) {
	d := New()
	_, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: "io.example.foreign.completed",
		Data: []byte(samplePayload),
	})
	if err == nil {
		t.Fatal("expected error for non-tekton event type")
	}
	if err.Error() != "not a tekton event: io.example.foreign.completed" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecode_InvalidJSON(t *testing.T) {
	d := New()
	_, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: typePipelineSuccessful,
		Data: []byte(`{invalid json`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDecode_EmptyPayload(t *testing.T) {
	d := New()
	_, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: typePipelineSuccessful,
		Data: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
	if err.Error() != "payload has neither taskRun nor pipelineRun" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecode_MissingProviderLabel(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
      "namespace": "` + testNamespaceCi + `",
      "labels": {},
      "annotations": {
        "scm.commit-sha": "abc123"
      }
    },
    "status": {}
  }
}`
	d := New()
	_, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: typePipelineSuccessful,
		Data: []byte(payload),
	})
	if err == nil {
		t.Fatal("expected error for missing provider label")
	}
	if err.Error() != "missing label scm.provider on ci/build-1" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecode_MissingCommitSHA(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
      "namespace": "` + testNamespaceCi + `",
      "labels": {"scm.provider": "` + testProviderGithub + `"},
      "annotations": {}
    },
    "status": {}
  }
}`
	d := New()
	_, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: typePipelineSuccessful,
		Data: []byte(payload),
	})
	if err == nil {
		t.Fatal("expected error for missing commit SHA")
	}
	if err.Error() != "missing annotation scm.commit-sha" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecode_WithAllRepoAnnotations(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
      "namespace": "` + testNamespaceCi + `",
      "labels": {"scm.provider": "azure"},
      "annotations": {
        "scm.repo-owner": "owner1",
        "scm.repo-name": "repo1",
        "scm.repo-id": "12345",
        "scm.repo-workspace": "ws1",
        "scm.repo-project": "proj1",
        "scm.repo-org": "org1",
        "scm.commit-sha": "abc123",
        "scm.api-base-url": "https://dev.azure.com",
        "scm.context": "custom/context"
      }
    },
    "status": {}
  }
}`
	d := New()
	env, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: typePipelineSuccessful,
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
}

func TestDecode_ContextFallback_PipelineRun(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "my-pipeline-123",
      "namespace": "` + testNamespaceCi + `",
      "labels": {"scm.provider": "` + testProviderGithub + `"},
      "annotations": {
        "scm.repo-owner": "` + testRepoOwner + `",
        "scm.repo-name": "` + testRepoName + `",
        "scm.commit-sha": "` + testCommitSHA + `"
      }
    },
    "status": {}
  }
}`
	d := New()
	env, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: typePipelineSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if env.Report.Context != "tekton/my-pipeline-123" {
		t.Errorf("Context = %q, want tekton/my-pipeline-123", env.Report.Context)
	}
}

func TestDecode_ContextFallback_TaskRun(t *testing.T) {
	payload := `{
  "taskRun": {
    "metadata": {
      "name": "my-task-456",
      "namespace": "` + testNamespaceCi + `",
      "labels": {"scm.provider": "` + testProviderGithub + `"},
      "annotations": {
        "scm.repo-owner": "` + testRepoOwner + `",
        "scm.repo-name": "` + testRepoName + `",
        "scm.commit-sha": "` + testCommitSHA + `"
      }
    },
    "status": {}
  }
}`
	d := New()
	env, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: "dev.tekton.event.taskrun.successful.v1",
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if env.Report.Context != "tekton/task/my-task-456" {
		t.Errorf("Context = %q, want tekton/task/my-task-456", env.Report.Context)
	}
}

func TestDecode_DescriptionFromCondition(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
      "namespace": "` + testNamespaceCi + `",
      "labels": {"scm.provider": "` + testProviderGithub + `"},
      "annotations": {
        "scm.commit-sha": "abc123"
      }
    },
    "status": {
      "conditions": [
        {"type":"Other","status":"True","reason":"","message":"Ignored"},
        {"type":"Succeeded","status":"False","reason":"Failed","message":"Tests failed in unit-test task"}
      ]
    }
  }
}`
	d := New()
	env, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: "dev.tekton.event.pipelinerun.failed.v1",
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if env.Report.Description != "Tests failed in unit-test task" {
		t.Errorf("Description = %q", env.Report.Description)
	}
}

func TestDecode_DescriptionTruncation(t *testing.T) {
	longMsg := "This is a very long message that exceeds 140 characters and should be truncated with ellipsis at the end to ensure it fits properly in status displays and notifications"
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
      "namespace": "` + testNamespaceCi + `",
      "labels": {"scm.provider": "` + testProviderGithub + `"},
      "annotations": {
        "scm.commit-sha": "abc123"
      }
    },
    "status": {
      "conditions": [
        {"type":"Succeeded","status":"True","reason":"Succeeded","message":"` + longMsg + `"}
      ]
    }
  }
}`
	d := New()
	env, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: typePipelineSuccessful,
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

func TestDecode_WithTimestamps(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
      "namespace": "` + testNamespaceCi + `",
      "labels": {"scm.provider": "` + testProviderGithub + `"},
      "annotations": {
        "scm.commit-sha": "abc123"
      }
    },
    "status": {
      "startTime": "2023-01-15T10:00:00Z",
      "completionTime": "2023-01-15T10:05:30Z"
    }
  }
}`
	d := New()
	env, err := d.Decode(event.RawEvent{
		ID:   testEventID,
		Type: typePipelineSuccessful,
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

func TestMapState_UnknownType(t *testing.T) {
	// MapState should return StatePending for unknown event types
	state := MapState("dev.tekton.event.pipelinerun.unknown-status.v1")
	if state != domain.StatePending {
		t.Errorf("MapState for unknown type = %q, want pending", state)
	}
}

func TestName(t *testing.T) {
	d := New()
	if d.Name() != "tekton" {
		t.Errorf("Name() = %q, want tekton", d.Name())
	}
}
