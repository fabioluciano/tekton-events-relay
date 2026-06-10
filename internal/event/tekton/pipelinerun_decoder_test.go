package tekton

import (
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

const (
	typePipelineSuccessful = "dev.tekton.event.pipelinerun.successful.v1"
	typePipelineFailed     = "dev.tekton.event.pipelinerun.failed.v1"

	testEventIDPipelinerun = "evt-1"
)

func TestPipelineRunDecoder_Name(t *testing.T) {
	d := NewPipelineRunDecoder()
	if d.Name() != decoderNamePipelineRun {
		t.Errorf("Name() = %q, want %s", d.Name(), decoderNamePipelineRun)
	}
}

func TestPipelineRunDecoder_CanHandle(t *testing.T) {
	d := NewPipelineRunDecoder()
	tests := []struct {
		eventType string
		want      bool
	}{
		{"dev.tekton.event.pipelinerun.started.v1", true},
		{"dev.tekton.event.pipelinerun.successful.v1", true},
		{"dev.tekton.event.pipelinerun.failed.v1", true},
		{"dev.tekton.event.taskrun.started.v1", false},
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

func TestPipelineRunDecoder_Decode_Success(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-pr-abc",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.repo-owner": "fabio",
        "tekton.dev/tekton-events-relay.scm.repo-name": "meu-repo",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123",
        "tekton.dev/tekton-events-relay.scm.context": "tekton/build"
      }
    },
    "status": {
      "conditions": [{"type":"Succeeded","status":"True","reason":"Succeeded","message":"All tasks passed"}]
    }
  }
}`

	d := NewPipelineRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1", //nolint:goconst
		Type: typePipelineSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if env.Report.Provider != "github" {
		t.Errorf("Provider = %q", env.Report.Provider)
	}
	if env.Report.State != domain.StateSuccess {
		t.Errorf("State = %q, want success", env.Report.State)
	}
	if env.Report.Resource != domain.ResourcePipelineRun {
		t.Errorf("Resource = %q", env.Report.Resource)
	}
	if env.Report.Description != "All tasks passed" {
		t.Errorf("Description = %q", env.Report.Description)
	}
}

func TestPipelineRunDecoder_Decode_AllStates(t *testing.T) {
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
			d := NewPipelineRunDecoder()
			env, err := d.Decode(event.RawEvent{
				ID:   testEventIDPipelinerun,
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

func TestPipelineRunDecoder_Decode_InvalidEventType(t *testing.T) {
	d := NewPipelineRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: "dev.tekton.event.taskrun.successful.v1", //nolint:goconst
		Data: []byte(`{"pipelineRun":{}}`),
	})
	if err == nil {
		t.Fatal("expected error for non-pipelinerun event type")
	}
	if err.Error() != "not a pipelinerun event: dev.tekton.event.taskrun.successful.v1" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPipelineRunDecoder_Decode_InvalidJSON(t *testing.T) {
	d := NewPipelineRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typePipelineSuccessful,
		Data: []byte(`{invalid json`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestPipelineRunDecoder_Decode_EmptyPayload(t *testing.T) {
	d := NewPipelineRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typePipelineSuccessful,
		Data: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
	if err.Error() != "payload has no pipelineRun" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPipelineRunDecoder_Decode_MissingProvider(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123"
      }
    },
    "status": {}
  }
}`
	d := NewPipelineRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typePipelineSuccessful,
		Data: []byte(payload),
	})
	if err == nil {
		t.Fatal("expected error for missing provider annotation")
	}
	if err.Error() != "missing annotation tekton.dev/tekton-events-relay.scm.provider on ci/build-1" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPipelineRunDecoder_Decode_MissingCommitSHA(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
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
	d := NewPipelineRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typePipelineSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("decode should succeed without commit SHA (for issue/discussion triggers): %v", err)
	}
	if env.Report.CommitSHA != "" {
		t.Errorf("expected empty SHA, got %q", env.Report.CommitSHA)
	}
}

func TestPipelineRunDecoder_Decode_ContextFallback(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "my-pipeline-123",
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
	d := NewPipelineRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
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

func TestPipelineRunDecoder_Decode_WithTimestamps(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
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
	d := NewPipelineRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
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

func TestPipelineRunDecoder_Decode_WithAllAnnotations(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
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
        "tekton.dev/pipeline": "main-pipeline"
      }
    },
    "status": {}
  }
}`
	d := NewPipelineRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
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

func TestPipelineRunDecoder_Decode_DescriptionFromCondition(t *testing.T) {
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123"
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
	d := NewPipelineRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typePipelineFailed,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if env.Report.Description != "Tests failed in unit-test task" {
		t.Errorf("Description = %q", env.Report.Description)
	}
}

func TestPipelineRunDecoder_Decode_DescriptionTruncation(t *testing.T) {
	longMsg := "This is a very long message that exceeds 140 characters and should be truncated with ellipsis at the end to ensure it fits properly in status displays and notifications"
	payload := `{
  "pipelineRun": {
    "metadata": {
      "name": "build-1",
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
	d := NewPipelineRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
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
