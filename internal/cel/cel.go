// Package cel provides CEL expression compilation and evaluation against domain.Event.
package cel

import (
	"fmt"

	"github.com/google/cel-go/cel"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// Program wraps a compiled CEL expression.
type Program struct {
	prog cel.Program
	expr string // original expression for debugging
}

// Compile compiles a CEL expression against the domain.Event schema.
// Returns error if expression is invalid or doesn't return bool.
func Compile(expr string) (*Program, error) {
	if expr == "" {
		return nil, fmt.Errorf("cel: empty expression")
	}

	env, err := cel.NewEnv(
		cel.Variable("event", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return nil, fmt.Errorf("cel: environment error: %w", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("cel: compile error: %w", issues.Err())
	}

	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("cel: expression must return bool, got %s", ast.OutputType())
	}

	prog, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("cel: program error: %w", err)
	}

	return &Program{prog: prog, expr: expr}, nil
}

// Eval evaluates the compiled program against an event.
// Returns (true, nil) if expression matches, (false, nil) if not,
// (false, error) if evaluation fails.
func (p *Program) Eval(e domain.Event) (bool, error) {
	issueNumber := 0
	if e.IssueNumber != nil {
		issueNumber = *e.IssueNumber
	}
	prNumber := 0
	if e.PRNumber != nil {
		prNumber = *e.PRNumber
	}

	activation := map[string]any{
		"event": map[string]any{
			"Resource":    string(e.Resource),
			"State":       string(e.State),
			"RunName":     e.RunName,
			"RunID":       e.RunID,
			"Namespace":   e.Namespace,
			"Context":     e.Context,
			"Description": e.Description,
			"CommitSHA":   e.CommitSHA,
			"Provider":    e.Provider,
			"Repo": map[string]string{
				"Owner":     e.Repo.Owner,
				"Name":      e.Repo.Name,
				"ID":        e.Repo.ID,
				"Workspace": e.Repo.Workspace,
				"Project":   e.Repo.Project,
				"Org":       e.Repo.Org,
			},
			"IssueNumber": issueNumber,
			"PRNumber":    prNumber,
		},
	}

	out, _, err := p.prog.Eval(activation)
	if err != nil {
		return false, fmt.Errorf("cel: eval error: %w", err)
	}

	result, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("cel: unexpected result type %T", out.Value())
	}

	return result, nil
}
