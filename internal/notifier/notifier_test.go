package notifier

import (
	"context"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const testProviderGithub = "github"

type stub struct {
	name       string
	actionType ActionType
	called     int
	err        error
}

func (s *stub) Name() string     { return s.name }
func (s *stub) Provider() string { return s.name }
func (s *stub) Type() ActionType { return s.actionType }
func (s *stub) Handle(_ context.Context, _ domain.Event) error {
	s.called++
	return s.err
}
func (s *stub) Close() error { return nil }

func TestRegistry_FanOut(t *testing.T) {
	reg := NewRegistry()
	a := &stub{name: "a", actionType: ActionCommitStatus}
	b := &stub{name: "b", actionType: ActionCommitStatus}
	reg.Register(a)
	reg.Register(b)

	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("got %d handlers, want 2", len(all))
	}
	for _, h := range all {
		_ = h.Handle(context.Background(), domain.Event{})
	}
	if a.called != 1 || b.called != 1 {
		t.Errorf("a=%d b=%d, want both 1", a.called, b.called)
	}
}

func TestRegistry_FindByName(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stub{name: testProviderGithub, actionType: ActionCommitStatus})

	handlers := reg.FindByName(testProviderGithub)
	if len(handlers) != 1 || handlers[0].Name() != testProviderGithub {
		t.Fatalf("FindByName failed, got %d handlers", len(handlers))
	}

	handlers = reg.FindByName("nope")
	if len(handlers) != 0 {
		t.Fatal("expected no handlers for unknown provider")
	}
}

func TestRegistry_FindByType(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stub{name: "github", actionType: ActionCommitStatus})
	reg.Register(&stub{name: "gitlab", actionType: ActionCommitStatus})
	reg.Register(&stub{name: "github", actionType: ActionIssueComment})

	statusHandlers := reg.FindByType(ActionCommitStatus)
	if len(statusHandlers) != 2 {
		t.Fatalf("FindByType(status) got %d, want 2", len(statusHandlers))
	}

	commentHandlers := reg.FindByType(ActionIssueComment)
	if len(commentHandlers) != 1 {
		t.Fatalf("FindByType(comment) got %d, want 1", len(commentHandlers))
	}
}

func TestRegistry_Names(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stub{name: "slack", actionType: ActionCommitStatus})
	reg.Register(&stub{name: testProviderGithub, actionType: ActionCommitStatus})
	reg.Register(&stub{name: testProviderGithub, actionType: ActionIssueComment}) // Same provider, different action

	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("Names() got %d, want 2 (github, slack)", len(names))
	}
	if names[0] != testProviderGithub || names[1] != "slack" {
		t.Errorf("Names not sorted: %v", names)
	}
}

func TestRegistry_HandlerNames(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stub{name: "slack", actionType: ActionNotify})
	reg.Register(&stub{name: testProviderGithub, actionType: ActionCommitStatus})
	reg.Register(&stub{name: testProviderGithub, actionType: ActionPRComment})

	names := reg.HandlerNames()
	if len(names) != 2 {
		t.Fatalf("HandlerNames() got %d, want 2", len(names))
	}

	expected := []string{
		"github/github[commit_status,pr_comment]",
		"slack/slack[notify]",
	}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("HandlerNames[%d] = %q, want %q", i, names[i], want)
		}
	}
}

func TestRegistry_MultipleHandlersPerProvider(t *testing.T) {
	reg := NewRegistry()
	status := &stub{name: testProviderGithub, actionType: ActionCommitStatus}
	comment := &stub{name: testProviderGithub, actionType: ActionIssueComment}
	label := &stub{name: testProviderGithub, actionType: ActionLabel}

	reg.Register(status)
	reg.Register(comment)
	reg.Register(label)

	handlers := reg.FindByName(testProviderGithub)
	if len(handlers) != 3 {
		t.Fatalf("Expected 3 handlers for github, got %d", len(handlers))
	}

	all := reg.All()
	if len(all) != 3 {
		t.Fatalf("Expected 3 total handlers, got %d", len(all))
	}
}
