package cel

import (
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func TestCompile_ResourceMatch(t *testing.T) {
	prog, err := Compile(`event.Resource == "taskrun"`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{Resource: domain.ResourceTaskRun}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true for matching resource")
	}

	ev.Resource = domain.ResourcePipelineRun
	result, err = prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false for non-matching resource")
	}
}

func TestCompile_CompositeExpression(t *testing.T) {
	prog, err := Compile(`event.State == "failure" && event.Resource == "pipelinerun"`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{
		State:    domain.StateFailure,
		Resource: domain.ResourcePipelineRun,
	}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true for matching composite")
	}

	ev.State = domain.StateSuccess
	result, err = prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false when state doesn't match")
	}
}

func TestCompile_RepoOwner(t *testing.T) {
	prog, err := Compile(`event.Repo.Owner == "myorg"`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{
		Repo: domain.Repo{Owner: "myorg"},
	}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true for matching repo owner")
	}
}

func TestCompile_NamespaceFilter(t *testing.T) {
	prog, err := Compile(`event.Namespace == "production"`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{Namespace: "production"}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true for matching namespace")
	}
}

func TestCompile_StringFunction(t *testing.T) {
	prog, err := Compile(`event.RunName.startsWith("nightly-")`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{RunName: "nightly-build-123"}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true for startsWith match")
	}

	ev.RunName = "daily-build-456"
	result, err = prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false for non-matching prefix")
	}
}

func TestCompile_InvalidSyntax(t *testing.T) {
	_, err := Compile(`invalid $$$ syntax`)
	if err == nil {
		t.Fatal("expected compile error for invalid syntax")
	}
}

func TestCompile_NonBoolExpression(t *testing.T) {
	_, err := Compile(`event.Resource`)
	if err == nil {
		t.Fatal("expected compile error for non-bool expression")
	}
}

func TestCompile_EmptyExpression(t *testing.T) {
	_, err := Compile("")
	if err == nil {
		t.Fatal("expected error for empty expression")
	}
}
