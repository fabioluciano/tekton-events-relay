package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
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

	return buildActionsWithMiddleware(inst.Actions, log, func(action config.Action) (notifier.ActionHandler, error) {
		return f.buildHandler(inst, action, token, log)
	})
}

// buildHandler creates the appropriate handler based on action type.
func (f *AzureFactory) buildHandler(inst config.AzureInstance, action config.Action, token string, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case config.ActionTypeCommitStatus:
		return azuredevops.NewStatusReporter(token, inst.BaseURL, inst.Genre, inst.InsecureSkipVerify, log), nil
	case config.ActionTypePRComment:
		if action.Mode == "upsert" {
			log.Warn("comment mode 'upsert' is not supported on Azure DevOps, using 'create'",
				zap.String("instance", inst.Name),
				zap.String("action", action.Name))
		}
		return azuredevops.NewCommentHandler(azuredevops.CommentConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Genre:              inst.Genre,
			Template:           action.Template,
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		})
	case config.ActionTypeLabel:
		return azuredevops.NewLabelHandler(azuredevops.LabelConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Genre:              inst.Genre,
			SuccessLabel:       action.SuccessLabel,
			FailureLabel:       action.FailureLabel,
			Labels:             labelSet(action),
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		}), nil
	default:
		return nil, ErrUnsupportedActionType
	}
}
