package github

// Common GitHub API constants used across handlers
const (
	// Provider name
	providerGitHub = "github"

	// GitHub Check Run statuses
	statusQueued     = "queued"
	statusInProgress = "in_progress"
	statusCompleted  = "completed"

	// GitHub Check Run and Deployment conclusions/states
	stateSuccess   = "success"
	stateFailure   = "failure"
	stateError     = "error"
	stateInactive  = "inactive"
	stateCancelled = "cancelled"

	// Authorization header prefix
	authBearerPrefix = "Bearer "
)
