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

func TestStateIn_MatchesOneOfMultiple(t *testing.T) {
	prog, err := Compile(`stateIn("failure", "error")`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{State: domain.StateFailure}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true when state is 'failure'")
	}
}

func TestStateIn_NoMatch(t *testing.T) {
	prog, err := Compile(`stateIn("failure", "error")`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{State: domain.StateSuccess}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false when state is 'success'")
	}
}

func TestStateIn_SingleState(t *testing.T) {
	prog, err := Compile(`stateIn("success")`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{State: domain.StateSuccess}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true when state matches single arg")
	}

	ev.State = domain.StateFailure
	result, err = prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false when state doesn't match single arg")
	}
}

func TestStateIn_MatchesSecondArg(t *testing.T) {
	prog, err := Compile(`stateIn("failure", "error")`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{State: domain.StateError}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true when state matches second arg 'error'")
	}
}

func TestMacro_NotMangledInStringLiteral(t *testing.T) {
	// This test proves that the string rewriting bug is fixed.
	// With string-based rewriting, "isPR() test case" would be corrupted.
	// With native macros, string literals are preserved.
	prog, err := Compile(`event.RunName == "isPR() test case"`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{RunName: "isPR() test case"}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true when RunName matches literal with isPR() inside")
	}

	ev.RunName = "different"
	result, err = prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false when RunName doesn't match")
	}
}

func TestMacro_IsPR(t *testing.T) {
	prog, err := Compile(`isPR()`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	prNum := 42
	ev := domain.Event{PRNumber: &prNum}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true when PRNumber != 0")
	}

	ev.PRNumber = nil
	result, err = prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false when PRNumber == 0")
	}
}

func TestMacro_IsIssue(t *testing.T) {
	prog, err := Compile(`isIssue()`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	issueNum := 123
	ev := domain.Event{IssueNumber: &issueNum}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true when IssueNumber != 0 and PRNumber == 0")
	}

	// Should be false when it's actually a PR
	prNum := 42
	ev.PRNumber = &prNum
	result, err = prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false when both IssueNumber and PRNumber are set")
	}
}

func TestMacro_IsTaskRun(t *testing.T) {
	prog, err := Compile(`isTaskRun()`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{Resource: domain.ResourceTaskRun}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true when Resource == taskrun")
	}

	ev.Resource = domain.ResourcePipelineRun
	result, err = prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false when Resource != taskrun")
	}
}

func TestMacro_IsPREvent(t *testing.T) {
	prog, err := Compile(`isPREvent()`)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	ev := domain.Event{SCMEventType: "pull_request"}
	result, err := prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true when SCMEventType == pull_request")
	}

	ev.SCMEventType = "push"
	result, err = prog.Eval(ev)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false when SCMEventType != pull_request")
	}
}
