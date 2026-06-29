package factory

import (
	"golang.org/x/time/rate"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/sourcehut"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// SourceHutFactory builds ActionHandlers from SourceHut instance configurations.
type SourceHutFactory struct{}

// Build creates action handlers for a single SourceHut instance.
func (f *SourceHutFactory) Build(inst config.SourceHutInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	token, err := resolveSourceHutToken(inst, log)
	if err != nil {
		return nil, err
	}

	handlers, err := buildActionsWithMiddleware(inst.Actions, log, func(action config.Action) (notifier.ActionHandler, error) {
		return f.buildHandler(inst, action, token, log)
	})
	if err != nil {
		return nil, err
	}
	if inst.RateLimit != nil {
		limiter := rate.NewLimiter(rate.Limit(inst.RateLimit.RequestsPerSecond), inst.RateLimit.Burst)
		for i, h := range handlers {
			handlers[i] = middleware.WrapWithRateLimit(h, limiter)
		}
	}
	return handlers, nil
}

// resolveSourceHutToken resolves the authentication token for a SourceHut instance.
func resolveSourceHutToken(inst config.SourceHutInstance, log *zap.Logger) (string, error) {
	if inst.Auth == nil {
		return secrets.ResolveOrInfer("", "sourcehut", inst.Name, "token", "", log)
	}
	return secrets.ResolveOrInfer(inst.Auth.SecretFile, "sourcehut", inst.Name, "token", inst.Auth.SecretKey, log)
}

// buildHandler creates the appropriate handler based on action type.
func (f *SourceHutFactory) buildHandler(inst config.SourceHutInstance, action config.Action, token string, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case notifier.ActionCommitStatus:
		return sourcehut.NewStatusReporter(token, inst.BaseURL, inst.InsecureSkipVerify, log), nil
	default:
		return nil, ErrUnsupportedActionType
	}
}
