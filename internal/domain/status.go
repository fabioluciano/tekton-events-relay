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
)

// UnmarshalText implements encoding.TextUnmarshaler for TOML array support.
// Allows TOML config like: on_states = ["failure", "success"]
func (s *State) UnmarshalText(text []byte) error {
	*s = State(string(text))
	return nil
}

// MarshalText implements encoding.TextMarshaler for TOML output.
func (s State) MarshalText() ([]byte, error) {
	return []byte(s), nil
}

// Resource identifies the type of resource that generated the event.
type Resource string

// Resource types
const (
	ResourceTaskRun     Resource = "taskrun"
	ResourcePipelineRun Resource = "pipelinerun"
)

// Repo holds all possible repository identifiers.
// Each SCM notifier uses a subset of these fields.
type Repo struct {
	Owner     string // GitHub, Gitea, SourceHut
	Name      string // all
	ID        string // GitLab (numeric project ID)
	Workspace string // Bitbucket Cloud
	Project   string // Bitbucket Server, Azure DevOps
	Org       string // Azure DevOps organization
}

// Event is the neutral payload routed to any Notifier.
// Renamed from StatusReport to reflect the expanded scope:
// SCM reporters use Repo+CommitSHA; chat/alerting use State+Description+Context.
type Event struct {
	// Routing
	Provider   string   // label scm.provider — used by SCM notifiers
	Resource   Resource // taskrun, pipelinerun, workflow
	APIBaseURL string   // base URL override (self-hosted SCMs)

	// Pipeline identity
	RunName   string // TaskRun/PipelineRun/Workflow name (metadata.name)
	RunID     string // TaskRun/PipelineRun unique identifier (metadata.uid)
	Namespace string // namespace Kubernetes

	// State
	State       State
	Context     string // logical check name (e.g.: "tekton/build")
	Description string // short message for humans
	TargetURL   string // clickable link (Tekton Dashboard)

	// SCM — used by commit status notifiers
	CommitSHA string
	Repo      Repo

	// Issue/PR linking (optional - nil if not available)
	// Extracted from Tekton annotations or enriched via API lookup
	IssueNumber *int `json:"issue_number,omitempty" toml:"issue_number,omitempty"`
	PRNumber    *int `json:"pr_number,omitempty" toml:"pr_number,omitempty"`

	// Timing
	StartedAt  time.Time
	FinishedAt time.Time
}
