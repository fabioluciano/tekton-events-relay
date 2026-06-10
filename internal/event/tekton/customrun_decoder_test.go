package tekton

import (
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

const (
	typeCustomRunSuccessful = "dev.tekton.event.customrun.successful.v1"
	typeCustomRunFailed     = "dev.tekton.event.customrun.failed.v1"
	typeCustomRunStarted    = "dev.tekton.event.customrun.started.v1"
	typeCustomRunRunning    = "dev.tekton.event.customrun.running.v1"
	typeCustomRunQueued     = "dev.tekton.event.customrun.queued.v1"

	testEventIDCustomrun = "evt-1"
)

func TestCustomRunDecoder_Name(t *testing.T) {
	d := NewCustomRunDecoder()
	if d.Name() != decoderNameCustomRun {
		t.Errorf("Name() = %q, want %s", d.Name(), decoderNameCustomRun)
	}
}

func TestCustomRunDecoder_CanHandle(t *testing.T) {
	d := NewCustomRunDecoder()

	tests := []struct {
		eventType string
		want      bool
	}{
		{"dev.tekton.event.customrun.successful.v1", true},
		{"dev.tekton.event.customrun.failed.v1", true},
		{"dev.tekton.event.customrun.started.v1", true},
		{"dev.tekton.event.customrun.running.v1", true},
		{"dev.tekton.event.customrun.queued.v1", true},
		{"dev.tekton.event.taskrun.successful.v1", false}, //nolint:goconst
		{"dev.tekton.event.pipelinerun.successful.v1", false},
		{"dev.tekton.event.triggers.started.v1", false},
		{"io.example.foreign.v1", false},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			if got := d.CanHandle(tt.eventType); got != tt.want {
				t.Errorf("CanHandle(%q) = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestCustomRunDecoder_Decode_Successful(t *testing.T) {
	payload := `{
  "customRun": {
    "metadata": {
      "name": "custom-task-123",
      "namespace": "ci",
      "uid": "custom-uid-123",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.repo-owner": "myorg",
        "tekton.dev/tekton-events-relay.scm.repo-name": "myrepo",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123",
        "tekton.dev/tekton-events-relay.scm.context": "tekton/custom-check"
      }
    },
    "status": {
      "startTime": "2023-01-15T10:00:00Z",
      "completionTime": "2023-01-15T10:05:00Z",
      "conditions": [
        {"type":"Succeeded","status":"True","reason":"Succeeded","message":"CustomRun completed successfully"}
      ]
    }
  }
}`

	d := NewCustomRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:     "evt-custom-1",
		Type:   typeCustomRunSuccessful,
		Source: "tekton-controller",
		Data:   []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if env.Report.Resource != domain.ResourceCustomRun {
		t.Errorf("Resource = %q, want customrun", env.Report.Resource)
	}
	if env.Report.Provider != "github" {
		t.Errorf("Provider = %q, want github", env.Report.Provider)
	}
	if env.Report.State != domain.StateSuccess {
		t.Errorf("State = %q, want success", env.Report.State)
	}
	if env.Report.RunName != "custom-task-123" {
		t.Errorf("RunName = %q", env.Report.RunName)
	}
	if env.Report.RunID != "custom-uid-123" {
		t.Errorf("RunID = %q", env.Report.RunID)
	}
	if env.Report.Namespace != "ci" {
		t.Errorf("Namespace = %q", env.Report.Namespace)
	}
	if env.Report.Context != "tekton/custom-check" {
		t.Errorf("Context = %q", env.Report.Context)
	}
	if env.Report.Description != "CustomRun completed successfully" {
		t.Errorf("Description = %q", env.Report.Description)
	}
	if env.CloudEventID != "evt-custom-1" {
		t.Errorf("CloudEventID = %q", env.CloudEventID)
	}
	if env.Source != "tekton-controller" {
		t.Errorf("Source = %q", env.Source)
	}

	expectedStart, _ := time.Parse(time.RFC3339, "2023-01-15T10:00:00Z")
	expectedEnd, _ := time.Parse(time.RFC3339, "2023-01-15T10:05:00Z")
	if !env.Report.StartedAt.Equal(expectedStart) {
		t.Errorf("StartedAt = %v, want %v", env.Report.StartedAt, expectedStart)
	}
	if !env.Report.FinishedAt.Equal(expectedEnd) {
		t.Errorf("FinishedAt = %v, want %v", env.Report.FinishedAt, expectedEnd)
	}
}

func TestCustomRunDecoder_Decode_Failed(t *testing.T) {
	payload := `{
  "customRun": {
    "metadata": {
      "name": "custom-fail-456",
      "namespace": "default",
      "uid": "custom-uid-456",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "gitlab",
        "tekton.dev/tekton-events-relay.scm.repo-owner": "testorg",
        "tekton.dev/tekton-events-relay.scm.repo-name": "testrepo",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "def456"
      }
    },
    "status": {
      "conditions": [
        {"type":"Succeeded","status":"False","reason":"Failed","message":"CustomRun execution failed"}
      ]
    }
  }
}`

	d := NewCustomRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-custom-2",
		Type: typeCustomRunFailed,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if env.Report.State != domain.StateFailure {
		t.Errorf("State = %q, want failure", env.Report.State)
	}
	if env.Report.Provider != "gitlab" {
		t.Errorf("Provider = %q", env.Report.Provider)
	}
}

func TestCustomRunDecoder_AllStates(t *testing.T) {
	tests := []struct {
		eventType string
		want      domain.State
	}{
		{typeCustomRunQueued, domain.StatePending},
		{typeCustomRunStarted, domain.StatePending},
		{typeCustomRunRunning, domain.StateRunning},
		{typeCustomRunSuccessful, domain.StateSuccess},
		{typeCustomRunFailed, domain.StateFailure},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			payload := `{
  "customRun": {
    "metadata": {
      "name": "custom-1",
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
			d := NewCustomRunDecoder()
			env, err := d.Decode(event.RawEvent{
				ID:   testEventIDCustomrun,
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

func TestCustomRunDecoder_ContextFallback(t *testing.T) {
	payload := `{
  "customRun": {
    "metadata": {
      "name": "my-custom-run-789",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123"
      }
    },
    "status": {}
  }
}`

	d := NewCustomRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1", //nolint:goconst
		Type: typeCustomRunSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if env.Report.Context != "tekton/customrun/my-custom-run-789" {
		t.Errorf("Context = %q, want tekton/customrun/my-custom-run-789", env.Report.Context)
	}
}

func TestCustomRunDecoder_WithTaskNameAnnotation(t *testing.T) {
	payload := `{
  "customRun": {
    "metadata": {
      "name": "custom-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123"
      },
      "labels": {
        "tekton.dev/task": "my-custom-task"
      }
    },
    "status": {}
  }
}`

	d := NewCustomRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeCustomRunSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if env.Report.TaskName != "my-custom-task" {
		t.Errorf("TaskName = %q, want my-custom-task", env.Report.TaskName)
	}
}

func TestCustomRunDecoder_WithTaskNameLabel(t *testing.T) {
	payload := `{
  "customRun": {
    "metadata": {
      "name": "custom-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123"
      },
      "labels": {
        "tekton.dev/task": "labeled-task"
      }
    },
    "status": {}
  }
}`

	d := NewCustomRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeCustomRunSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if env.Report.TaskName != "labeled-task" {
		t.Errorf("TaskName = %q, want labeled-task", env.Report.TaskName)
	}
}

func TestCustomRunDecoder_MissingProvider(t *testing.T) {
	payload := `{
  "customRun": {
    "metadata": {
      "name": "custom-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123"
      }
    },
    "status": {}
  }
}`

	d := NewCustomRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeCustomRunSuccessful,
		Data: []byte(payload),
	})
	if err == nil {
		t.Fatal("expected error for missing provider annotation")
	}
	if err.Error() != "missing annotation tekton.dev/tekton-events-relay.scm.provider on ci/custom-1" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCustomRunDecoder_MissingCommitSHA(t *testing.T) {
	payload := `{
  "customRun": {
    "metadata": {
      "name": "custom-1",
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

	d := NewCustomRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeCustomRunSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("decode should succeed without commit SHA (for issue/discussion triggers): %v", err)
	}
	if env.Report.CommitSHA != "" {
		t.Errorf("expected empty SHA, got %q", env.Report.CommitSHA)
	}
}

func TestCustomRunDecoder_EmptyPayload(t *testing.T) {
	d := NewCustomRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeCustomRunSuccessful,
		Data: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
	if err.Error() != "payload has no customRun" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCustomRunDecoder_InvalidJSON(t *testing.T) {
	d := NewCustomRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeCustomRunSuccessful,
		Data: []byte(`{invalid json`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCustomRunDecoder_InvalidEventType(t *testing.T) {
	d := NewCustomRunDecoder()
	_, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: "dev.tekton.event.taskrun.successful.v1",
		Data: []byte(`{"customRun":{}}`),
	})
	if err == nil {
		t.Fatal("expected error for non-customrun event type")
	}
}

func TestCustomRunDecoder_WithAllRepoAnnotations(t *testing.T) {
	payload := `{
  "customRun": {
    "metadata": {
      "name": "custom-1",
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
        "tekton.dev/tekton-events-relay.scm.api-base-url": "https://dev.azure.com"
      }
    },
    "status": {}
  }
}`

	d := NewCustomRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeCustomRunSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	repo := env.Report.Repo
	if repo.Owner != "owner1" { //nolint:goconst // test fixture
		t.Errorf("Repo.Owner = %q", repo.Owner)
	}
	if repo.Name != "repo1" { //nolint:goconst // test fixture
		t.Errorf("Repo.Name = %q", repo.Name)
	}
	if repo.ID != "12345" {
		t.Errorf("Repo.ID = %q", repo.ID)
	}
	if repo.Workspace != "ws1" { //nolint:goconst // test fixture
		t.Errorf("Repo.Workspace = %q", repo.Workspace)
	}
	if repo.Project != "proj1" { //nolint:goconst // test fixture
		t.Errorf("Repo.Project = %q", repo.Project)
	}
	if repo.Org != "org1" { //nolint:goconst // test fixture
		t.Errorf("Repo.Org = %q", repo.Org)
	}
	if env.Report.APIBaseURL != "https://dev.azure.com" { //nolint:goconst // test fixture
		t.Errorf("APIBaseURL = %q", env.Report.APIBaseURL)
	}
}

func TestCustomRunDecoder_WithIssueNumbers(t *testing.T) {
	payload := `{
  "customRun": {
    "metadata": {
      "name": "custom-1",
      "namespace": "ci",
      "annotations": {
        "tekton.dev/tekton-events-relay.scm.provider": "github",
        "tekton.dev/tekton-events-relay.scm.commit-sha": "abc123",
        "tekton.dev/tekton-events-relay.scm.issue-number": "42",
        "tekton.dev/tekton-events-relay.scm.pr-number": "123",
        "tekton.dev/tekton-events-relay.scm.discussion-number": "7"
      }
    },
    "status": {}
  }
}`

	d := NewCustomRunDecoder()
	env, err := d.Decode(event.RawEvent{
		ID:   "evt-1",
		Type: typeCustomRunSuccessful,
		Data: []byte(payload),
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if env.Report.IssueNumber == nil || *env.Report.IssueNumber != 42 {
		t.Errorf("IssueNumber = %v, want 42", env.Report.IssueNumber)
	}
	if env.Report.PRNumber == nil || *env.Report.PRNumber != 123 {
		t.Errorf("PRNumber = %v, want 123", env.Report.PRNumber)
	}
	if env.Report.DiscussionNumber == nil || *env.Report.DiscussionNumber != 7 {
		t.Errorf("DiscussionNumber = %v, want 7", env.Report.DiscussionNumber)
	}
}
