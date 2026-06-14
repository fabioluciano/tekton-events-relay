package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/gitea"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// GiteaFactory builds ActionHandlers from Gitea instance configurations.
type GiteaFactory struct{}

// Build creates action handlers for a single Gitea instance.
func (f *GiteaFactory) Build(inst config.GiteaInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	token, err := resolveGiteaToken(inst, log)
	if err != nil {
		return nil, err
	}

	return buildActionsWithMiddleware(inst.Actions, log, func(action config.Action) (notifier.ActionHandler, error) {
		return f.buildHandler(inst, action, token, log)
	})
}

// resolveGiteaToken resolves the authentication token for a Gitea instance.
func resolveGiteaToken(inst config.GiteaInstance, log *zap.Logger) (string, error) {
	if inst.Auth == nil {
		return secrets.ResolveOrInfer("", "gitea", inst.Name, "token", "", log)
	}
	if inst.Auth.OAuth2 != nil {
		return resolveOAuth2Token(inst.Auth.OAuth2, "gitea", inst.Name, log)
	}
	return secrets.ResolveOrInfer(inst.Auth.SecretFile, "gitea", inst.Name, "token", inst.Auth.SecretKey, log)
}

// buildHandler creates the appropriate handler based on action type.
func (f *GiteaFactory) buildHandler(inst config.GiteaInstance, action config.Action, token string, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case notifier.ActionCommitStatus:
		return gitea.NewStatusReporter(token, inst.BaseURL, inst.InsecureSkipVerify, log)
	case notifier.ActionPRComment:
		return gitea.NewPRCommentHandler(gitea.PRCommentConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Template:           action.Template,
			Mode:               action.Mode,
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		})
	case notifier.ActionIssueComment:
		return gitea.NewIssueCommentHandler(gitea.IssueCommentConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Template:           action.Template,
			Mode:               action.Mode,
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		})
	case notifier.ActionLabel:
		return gitea.NewLabelHandler(gitea.LabelConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Labels:             labelSet(action),
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		})
	default:
		return nil, ErrUnsupportedActionType
	}
}
