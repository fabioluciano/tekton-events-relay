// Package domain defines the neutral event model, independent of any
// notifier or decoder. All adapters convert to/from this type.
package domain

import "time"

// State represents the logical state of an execution.
type State string

// Execution states
const (
	StatePending  State = "pending"
	StateRunning  State = "running"
	StateSuccess  State = "success"
	StateFailure  State = "failure"
	StateError    State = "error"
	StateCanceled State = "canceled"
	StateDone     State = "done" // EventListener completion state
)

// UnmarshalText implements encoding.TextUnmarshaler for YAML array support.
// Allows YAML config like: on_states = ["failure", "success"]
func (s *State) UnmarshalText(text []byte) error {
	*s = State(string(text))
	return nil
}

// MarshalText implements encoding.TextMarshaler for YAML output.
func (s State) MarshalText() ([]byte, error) {
	return []byte(s), nil
}

// Resource identifies the type of resource that generated the event.
type Resource string

// Resource types
const (
	ResourceTaskRun       Resource = "taskrun"
	ResourcePipelineRun   Resource = "pipelinerun"
	ResourceCustomRun     Resource = "customrun"
	ResourceEventListener Resource = "eventlistener"
)

// Repo holds all possible repository identifiers.
// Each SCM notifier uses a subset of these fields.
type Repo struct {
	Owner     string `json:"owner" yaml:"owner"`         // GitHub, Gitea, SourceHut
	Name      string `json:"name" yaml:"name"`           // all
	ID        string `json:"id" yaml:"id"`               // GitLab (numeric project ID)
	Workspace string `json:"workspace" yaml:"workspace"` // Bitbucket Cloud
	Project   string `json:"project" yaml:"project"`     // Bitbucket Server, Azure DevOps
	Org       string `json:"org" yaml:"org"`             // Azure DevOps organization
}

// Result represents a single result from a TaskRun or PipelineRun.
// Corresponds to Tekton's result model (name + value pairs).
type Result struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

// Event is the neutral payload routed to any Notifier.
// Renamed from StatusReport to reflect the expanded scope:
// SCM reporters use Repo+CommitSHA; chat/alerting use State+Description+Context.
type Event struct {
	// Routing
	Provider   string   `json:"provider" yaml:"provider"`         // annotation tekton.dev/tekton-events-relay.scm.provider — used by SCM notifiers
	Resource   Resource `json:"resource" yaml:"resource"`         // taskrun, pipelinerun, workflow
	APIBaseURL string   `json:"api_base_url" yaml:"api_base_url"` // base URL override (self-hosted SCMs)

	// Pipeline identity
	RunName   string `json:"run_name" yaml:"run_name"`   // TaskRun/PipelineRun/Workflow name (metadata.name)
	RunID     string `json:"run_id" yaml:"run_id"`       // TaskRun/PipelineRun unique identifier (metadata.uid)
	Namespace string `json:"namespace" yaml:"namespace"` // namespace Kubernetes

	// Resource-specific names (for filtering)
	TaskName          string `json:"task_name" yaml:"task_name"`                     // Referenced Task spec name (not TaskRun name)
	PipelineName      string `json:"pipeline_name" yaml:"pipeline_name"`             // Referenced Pipeline spec name (not PipelineRun name)
	PipelineTaskName  string `json:"pipeline_task_name" yaml:"pipeline_task_name"`   // Task name within Pipeline (tekton.dev/pipelineTask label)
	EventListenerName string `json:"event_listener_name" yaml:"event_listener_name"` // EventListener that processed the trigger
	TriggerName       string `json:"trigger_name" yaml:"trigger_name"`               // Tekton Trigger name that created the run (triggers.tekton.dev/trigger label)

	// Display names (optional - for UI rendering)
	TaskDisplayName     string `json:"task_display_name" yaml:"task_display_name"`         // Task displayName from spec
	PipelineDisplayName string `json:"pipeline_display_name" yaml:"pipeline_display_name"` // Pipeline displayName from spec

	// Finally flag (optional - indicates if task is part of finally block)
	IsFinallyTask bool `json:"is_finally_task" yaml:"is_finally_task"` // true if tekton.dev/memberOf label = "finally"

	// SCM webhook event type (optional - populated from EventListener started.v1 webhook headers)
	// Examples: "issues", "pull_request", "issue_comment", "push" (GitHub)
	SCMEventType string `json:"scm_event_type" yaml:"scm_event_type"`

	// Execution metrics
	TaskCount int `json:"task_count" yaml:"task_count"` // Number of child tasks executed (from status.childReferences length)

	// State
	State       State  `json:"state" yaml:"state"`
	Context     string `json:"context" yaml:"context"`         // logical check name (e.g.: "tekton/build")
	Description string `json:"description" yaml:"description"` // short message for humans
	TargetURL   string `json:"target_url" yaml:"target_url"`   // clickable link (Tekton Dashboard)

	// SCM — used by commit status notifiers
	CommitSHA string `json:"commit_sha" yaml:"commit_sha"`
	Repo      Repo   `json:"repo" yaml:"repo"`

	// Issue/PR/Discussion linking (optional - nil if not available)
	// Extracted from Tekton annotations or enriched via API lookup
	IssueNumber      *int `json:"issue_number,omitempty" yaml:"issue_number,omitempty"`
	PRNumber         *int `json:"pr_number,omitempty" yaml:"pr_number,omitempty"`
	DiscussionNumber *int `json:"discussion_number,omitempty" yaml:"discussion_number,omitempty"`

	// Results (optional - populated from TaskRun.status.taskResults or PipelineRun.status.pipelineResults)
	Results []Result `json:"results,omitempty" yaml:"results,omitempty"`

	// Timing
	StartedAt  time.Time `json:"started_at" yaml:"started_at"`
	FinishedAt time.Time `json:"finished_at" yaml:"finished_at"`
}
