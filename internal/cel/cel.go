// Package cel provides CEL expression compilation and evaluation against domain.Event.
package cel

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/parser"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// activationPool reuses activation map allocations to reduce GC pressure in hot path.
var activationPool = sync.Pool{
	New: func() any {
		return make(map[string]any, 1)
	},
}

// domainMacros defines all custom CEL macros for the domain.Event schema.
var domainMacros = []cel.EnvOption{
	// Resource type helpers
	cel.Macros(
		cel.GlobalMacro("isPR", 0, buildNotEqualZeroMacro("PRNumber")),
		cel.GlobalMacro("isDiscussion", 0, buildNotEqualZeroMacro("DiscussionNumber")),
		cel.GlobalMacro("isIssue", 0, isIssueMacroExpander),
		cel.GlobalMacro("isTaskRun", 0, buildResourceEqualityMacro("taskrun")),
		cel.GlobalMacro("isPipelineRun", 0, buildResourceEqualityMacro("pipelinerun")),
		cel.GlobalMacro("isCustomRun", 0, buildResourceEqualityMacro("customrun")),
		cel.GlobalMacro("isEventListener", 0, buildResourceEqualityMacro("eventlistener")),
		cel.GlobalMacro("isFinallyTask", 0, buildBoolFieldMacro("IsFinallyTask")),
		// SCM webhook event type helpers
		cel.GlobalMacro("isIssueEvent", 0, buildSCMEventTypeMacro("issues")),
		cel.GlobalMacro("isPREvent", 0, buildSCMEventTypeMacro("pull_request")),
		cel.GlobalMacro("isCommentEvent", 0, buildSCMEventTypeMacro("issue_comment")),
		cel.GlobalMacro("isPushEvent", 0, buildSCMEventTypeMacro("push")),
		// stateIn vararg macro
		cel.GlobalVarArgMacro("stateIn", stateInMacroExpander),
	),
}

// buildNotEqualZeroMacro creates a macro that expands to event.<field> != 0.
func buildNotEqualZeroMacro(field string) parser.MacroExpander {
	return func(eh parser.ExprHelper, _ ast.Expr, _ []ast.Expr) (ast.Expr, *common.Error) {
		// event.<field>
		eventIdent := eh.NewIdent("event")
		fieldAccess := eh.NewSelect(eventIdent, field)
		// 0
		zero := eh.NewLiteral(types.Int(0))
		// event.<field> != 0
		return eh.NewCall(operators.NotEquals, fieldAccess, zero), nil
	}
}

// buildResourceEqualityMacro creates a macro that expands to event.Resource == "<value>".
func buildResourceEqualityMacro(value string) parser.MacroExpander {
	return func(eh parser.ExprHelper, _ ast.Expr, _ []ast.Expr) (ast.Expr, *common.Error) {
		// event.Resource
		eventIdent := eh.NewIdent("event")
		resourceField := eh.NewSelect(eventIdent, "Resource")
		// "<value>"
		resourceValue := eh.NewLiteral(types.String(value))
		// event.Resource == "<value>"
		return eh.NewCall(operators.Equals, resourceField, resourceValue), nil
	}
}

// buildBoolFieldMacro creates a macro that expands to event.<field> == true.
func buildBoolFieldMacro(field string) parser.MacroExpander {
	return func(eh parser.ExprHelper, _ ast.Expr, _ []ast.Expr) (ast.Expr, *common.Error) {
		// event.<field>
		eventIdent := eh.NewIdent("event")
		fieldAccess := eh.NewSelect(eventIdent, field)
		// true
		trueVal := eh.NewLiteral(types.Bool(true))
		// event.<field> == true
		return eh.NewCall(operators.Equals, fieldAccess, trueVal), nil
	}
}

// buildSCMEventTypeMacro creates a macro that expands to event.SCMEventType == "<value>".
func buildSCMEventTypeMacro(value string) parser.MacroExpander {
	return func(eh parser.ExprHelper, _ ast.Expr, _ []ast.Expr) (ast.Expr, *common.Error) {
		// event.SCMEventType
		eventIdent := eh.NewIdent("event")
		scmEventTypeField := eh.NewSelect(eventIdent, "SCMEventType")
		// "<value>"
		eventTypeValue := eh.NewLiteral(types.String(value))
		// event.SCMEventType == "<value>"
		return eh.NewCall(operators.Equals, scmEventTypeField, eventTypeValue), nil
	}
}

// isIssueMacroExpander expands isIssue() to (event.IssueNumber != 0 && event.PRNumber == 0 && event.DiscussionNumber == 0).
func isIssueMacroExpander(eh parser.ExprHelper, _ ast.Expr, _ []ast.Expr) (ast.Expr, *common.Error) {
	eventIdent := eh.NewIdent("event")
	zero := eh.NewLiteral(types.Int(0))

	// event.IssueNumber != 0
	issueField := eh.NewSelect(eventIdent, "IssueNumber")
	issueCheck := eh.NewCall(operators.NotEquals, issueField, zero)

	// event.PRNumber == 0
	prField := eh.NewSelect(eh.NewIdent("event"), "PRNumber")
	prCheck := eh.NewCall(operators.Equals, prField, zero)

	// event.DiscussionNumber == 0
	discussionField := eh.NewSelect(eh.NewIdent("event"), "DiscussionNumber")
	discussionCheck := eh.NewCall(operators.Equals, discussionField, zero)

	// Combine: issueCheck && prCheck && discussionCheck
	combined := eh.NewCall(operators.LogicalAnd, issueCheck, prCheck)
	return eh.NewCall(operators.LogicalAnd, combined, discussionCheck), nil
}

// stateInMacroExpander expands stateIn("a", "b", ...) to event.State in ["a", "b", ...].
func stateInMacroExpander(eh parser.ExprHelper, _ ast.Expr, args []ast.Expr) (ast.Expr, *common.Error) {
	if len(args) == 0 {
		return nil, eh.NewError(0, "stateIn() requires at least one argument")
	}

	// event.State
	eventIdent := eh.NewIdent("event")
	stateField := eh.NewSelect(eventIdent, "State")

	// Build list [args...]
	stateList := eh.NewList(args...)

	// event.State in [args...]
	return eh.NewCall(operators.In, stateField, stateList), nil
}

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

	envOpts := []cel.EnvOption{
		cel.Variable("event", cel.MapType(cel.StringType, cel.DynType)),
	}
	envOpts = append(envOpts, domainMacros...)

	env, err := cel.NewEnv(envOpts...)
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
	// Get activation map from pool to reduce allocations
	activation := activationPool.Get().(map[string]any)
	populateActivation(activation, e)

	out, _, err := p.prog.Eval(activation)

	// Clear and return to pool
	for k := range activation {
		delete(activation, k)
	}
	activationPool.Put(activation)

	if err != nil {
		return false, fmt.Errorf("cel: eval error: %w", err)
	}

	result, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("cel: unexpected result type %T", out.Value())
	}

	return result, nil
}

// populateActivation populates the CEL activation map from a domain.Event.
// The caller is responsible for clearing the map before returning it to the pool.
func populateActivation(activation map[string]any, e domain.Event) {
	issueNumber := 0
	if e.IssueNumber != nil {
		issueNumber = *e.IssueNumber
	}
	prNumber := 0
	if e.PRNumber != nil {
		prNumber = *e.PRNumber
	}
	discussionNumber := 0
	if e.DiscussionNumber != nil {
		discussionNumber = *e.DiscussionNumber
	}

	results := make(map[string]string, len(e.Results))
	for _, r := range e.Results {
		results[r.Name] = r.Value
	}

	activation["event"] = map[string]any{
		"Resource":            string(e.Resource),
		"State":               string(e.State),
		"RunName":             e.RunName,
		"RunID":               e.RunID,
		"Namespace":           e.Namespace,
		"Context":             e.Context,
		"Description":         e.Description,
		"CommitSHA":           e.CommitSHA,
		"Provider":            e.Provider,
		"APIBaseURL":          e.APIBaseURL,
		"TaskName":            e.TaskName,
		"PipelineName":        e.PipelineName,
		"PipelineTaskName":    e.PipelineTaskName,
		"EventListenerName":   e.EventListenerName,
		"TriggerName":         e.TriggerName,
		"TaskDisplayName":     e.TaskDisplayName,
		"PipelineDisplayName": e.PipelineDisplayName,
		"TaskCount":           e.TaskCount,
		"TargetURL":           e.TargetURL,
		"Results":             results,
		"StartedAt":           e.StartedAt,
		"FinishedAt":          e.FinishedAt,
		"Repo": map[string]string{
			"Owner":     e.Repo.Owner,
			"Name":      e.Repo.Name,
			"ID":        e.Repo.ID,
			"Workspace": e.Repo.Workspace,
			"Project":   e.Repo.Project,
			"Org":       e.Repo.Org,
		},
		"IssueNumber":      issueNumber,
		"PRNumber":         prNumber,
		"DiscussionNumber": discussionNumber,
		"IsFinallyTask":    e.IsFinallyTask,
		"SCMEventType":     e.SCMEventType,
	}
}
