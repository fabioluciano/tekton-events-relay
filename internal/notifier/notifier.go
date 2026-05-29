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
	ActionCommitStatus ActionType = "commit_status"
	ActionIssueComment ActionType = "issue_comment"
	ActionPRComment    ActionType = "pr_comment"
	ActionLabel        ActionType = "label"
)

// ActionHandler is the new interface for action-specific handlers.
// Replaces Notifier interface to support multiple actions per provider.
type ActionHandler interface {
	// Name returns the provider identifier (github, gitlab, etc.)
	Name() string
	// Type returns the action type (commit_status, issue_comment, etc.)
	Type() ActionType
	// Handle processes the event. Returns nil if skipped (provider mismatch, missing fields).
	Handle(ctx context.Context, e domain.Event) error
}

// Notifier is the legacy Strategy interface. Deprecated: use ActionHandler.
// Kept for backward compatibility with chat/alerting notifiers.
type Notifier interface {
	// Name returns the notifier identifier (for logs/health).
	Name() string
	// Notify sends the event to the destination.
	Notify(ctx context.Context, e domain.Event) error
}

// NotifierAdapter wraps legacy Notifier into ActionHandler for backward compatibility.
// Used for chat/alerting notifiers (Slack, Teams, Discord, PagerDuty, Datadog, Webhook).
//
// Migration path: Once chat/webhook/monitoring implement ActionHandler directly,
// remove this adapter and legacy Notifier interface. SCM providers already use
// ActionHandler exclusively and do not require this adapter.
type NotifierAdapter struct {
	notifier Notifier
}

// WrapNotifier wraps a legacy Notifier into an ActionHandler.
func WrapNotifier(n Notifier) ActionHandler {
	return &NotifierAdapter{notifier: n}
}

func (a *NotifierAdapter) Name() string                           { return a.notifier.Name() }
func (a *NotifierAdapter) Type() ActionType                       { return ActionCommitStatus }
func (a *NotifierAdapter) Handle(ctx context.Context, e domain.Event) error {
	return a.notifier.Notify(ctx, e)
}

// Registry maintains all registered ActionHandlers. Thread-safe.
// BREAKING CHANGE: byName changed from map[string]Notifier to map[string][]ActionHandler.
// Multiple handlers can be registered per provider (e.g., github has status, issue_comment, label handlers).
type Registry struct {
	mu       sync.RWMutex
	handlers []ActionHandler
	byName   map[string][]ActionHandler  // provider → handlers
	byType   map[ActionType][]ActionHandler // action type → handlers
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

// Names returns the names of all providers, sorted and deduplicated.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
