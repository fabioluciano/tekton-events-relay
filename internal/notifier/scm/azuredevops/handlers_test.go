package azuredevops

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

func azureEvent() domain.Event {
	return domain.Event{
		Provider:    providerAzure,
		Repo:        domain.Repo{Org: "org", Project: "proj", Name: "repo"},
		CommitSHA:   "abc123",
		RunName:     "run-1",
		State:       domain.StateSuccess,
		Context:     "tekton/ci",
		Description: "done",
	}
}

func TestStatusReporter_NameAndType(t *testing.T) {
	r := NewStatusReporter("token", "https://dev.azure.example.com", "tekton", false, zap.NewNop())
	if r.Name() != "azure-devops" {
		t.Errorf("Name = %q, want azure-devops", r.Name())
	}
	if r.Type() != notifier.ActionCommitStatus {
		t.Errorf("Type = %q, want commit_status", r.Type())
	}
}

func TestStatusReporter_SkipsWrongProvider(t *testing.T) {
	r := NewStatusReporter("token", "https://dev.azure.example.com", "tekton", false, zap.NewNop())
	e := azureEvent()
	e.Provider = "github"
	// Must skip before any network access — error would mean an API call.
	if err := r.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
}

func TestStatusReporter_SkipsMissingFields(t *testing.T) {
	r := NewStatusReporter("token", "https://dev.azure.example.com", "tekton", false, zap.NewNop())
	for _, mutate := range []func(*domain.Event){
		func(e *domain.Event) { e.Repo.Org = "" },
		func(e *domain.Event) { e.Repo.Project = "" },
		func(e *domain.Event) { e.Repo.Name = "" },
		func(e *domain.Event) { e.CommitSHA = "" },
	} {
		e := azureEvent()
		mutate(&e)
		if err := r.Handle(context.Background(), e); err != nil {
			t.Fatalf("Handle should skip silently, got: %v", err)
		}
	}
}

func TestAzureStateMap(t *testing.T) {
	cases := map[domain.State]string{
		domain.StatePending: "pending",
		domain.StateRunning: "pending",
		domain.StateSuccess: "succeeded",
		domain.StateFailure: "failed",
		domain.StateError:   "error",
	}
	for in, want := range cases {
		if got := azureStateMap.Map(in, "pending"); got != want {
			t.Errorf("Map(%s) = %s, want %s", in, got, want)
		}
	}
}
