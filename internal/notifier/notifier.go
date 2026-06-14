// Package notifier defines the common Strategy interface for all notification
// destinations (SCM, Slack, Teams, Discord, PagerDuty, Datadog, Webhook)
// and the Registry that resolves which notifiers to call for each event.
package notifier

import (
	"context"
	"sort"
	"sync"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// ActionType identifies the type of action a handler performs.
type ActionType string

// Action types supported across SCM providers.
const (
	ActionCommitStatus      ActionType = "commit_status"
	ActionCommitComment     ActionType = "commit_comment"
	ActionIssueComment      ActionType = "issue_comment"
	ActionPRComment         ActionType = "pr_comment"
	ActionLabel             ActionType = "label"
	ActionCheckRun          ActionType = "check_run"
	ActionDiscussionComment ActionType = "discussion_comment"
	ActionDeploymentStatus  ActionType = "deployment_status"
	ActionNotify            ActionType = "notify" // for generic notifiers (Slack, Teams, Discord, PagerDuty, Datadog, Webhook)
	ActionJiraComment       ActionType = "jira_comment"
	ActionJiraTransition    ActionType = "jira_transition"
)

// ActionHandler is the interface for action-specific handlers.
// Supports multiple actions per provider.
type ActionHandler interface {
	// Name returns the provider identifier (github, gitlab, etc.)
	Name() string
	// Type returns the action type (commit_status, issue_comment, etc.)
	Type() ActionType
	// Handle processes the event. Returns nil if skipped (provider mismatch, missing fields).
	Handle(ctx context.Context, e domain.Event) error
}

// Registry maintains all registered ActionHandlers. Thread-safe.
// BREAKING CHANGE: byName changed from map[string]Notifier to map[string][]ActionHandler.
// Multiple handlers can be registered per provider (e.g., github has status, issue_comment, label handlers).
type Registry struct {
	mu       sync.RWMutex
	handlers []ActionHandler
	byName   map[string][]ActionHandler     // provider → handlers
	byType   map[ActionType][]ActionHandler // action type → handlers
	names    []string                       // cached sorted names
}

// NewRegistry creates a new Registry for action handlers.
func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string][]ActionHandler),
		byType: make(map[ActionType][]ActionHandler),
	}
}

// Register adds an ActionHandler. Appends to the provider's handler list.
func (r *Registry) Register(h ActionHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers = append(r.handlers, h)
	r.byName[h.Name()] = append(r.byName[h.Name()], h)
	r.byType[h.Type()] = append(r.byType[h.Type()], h)
	r.names = nil // invalidate cache
}

// FindByName returns all handlers for a given provider name.
func (r *Registry) FindByName(name string) []ActionHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byName[name]
}

// FindByType returns all handlers for a given action type.
func (r *Registry) FindByType(t ActionType) []ActionHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byType[t]
}

// All returns all registered ActionHandlers (used by Dispatcher fan-out).
func (r *Registry) All() []ActionHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ActionHandler, len(r.handlers))
	copy(out, r.handlers)
	return out
}

// Lookup returns the first registered handler with the given name, or nil if not found.
func (r *Registry) Lookup(name string) ActionHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handlers := r.byName[name]
	if len(handlers) == 0 {
		return nil
	}
	return handlers[0]
}

// Names returns the names of all providers, sorted and deduplicated.
func (r *Registry) Names() []string {
	r.mu.RLock()
	if r.names != nil {
		out := make([]string, len(r.names))
		copy(out, r.names)
		r.mu.RUnlock()
		return out
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check after write lock.
	if r.names != nil {
		out := make([]string, len(r.names))
		copy(out, r.names)
		return out
	}
	r.names = make([]string, 0, len(r.byName))
	for name := range r.byName {
		r.names = append(r.names, name)
	}
	sort.Strings(r.names)
	out := make([]string, len(r.names))
	copy(out, r.names)
	return out
}
