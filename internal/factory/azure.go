package factory

import (
	"golang.org/x/time/rate"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/azuredevops"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// AzureFactory builds ActionHandlers from Azure DevOps instance configurations.
type AzureFactory struct{}

// Build creates action handlers for a single Azure DevOps instance.
func (f *AzureFactory) Build(inst config.AzureInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	// Resolve token from volume mount
	token, err := secrets.ResolveOrInfer(inst.SecretFile, "azure", inst.Name, "token", inst.SecretKey, log)
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

// buildHandler creates the appropriate handler based on action type.
func (f *AzureFactory) buildHandler(inst config.AzureInstance, action config.Action, token string, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case notifier.ActionCommitStatus:
		return azuredevops.NewStatusReporter(inst.Name, token, inst.BaseURL, inst.Genre, inst.InsecureSkipVerify, log), nil
	case notifier.ActionPRComment:
		return azuredevops.NewCommentHandler(azuredevops.CommentConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Genre:              inst.Genre,
			Template:           action.Template,
			Mode:               action.Mode,
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		})
	case notifier.ActionLabel:
		return azuredevops.NewLabelHandler(azuredevops.LabelConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Genre:              inst.Genre,
			Labels:             labelSet(action),
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		}), nil
	default:
		return nil, ErrUnsupportedActionType
	}
}
