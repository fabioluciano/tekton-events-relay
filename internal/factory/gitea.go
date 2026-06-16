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

	client, err := resolveGiteaClient(inst, log)
	if err != nil {
		return nil, err
	}

	return buildActionsWithMiddleware(inst.Actions, log, func(action config.Action) (notifier.ActionHandler, error) {
		return f.buildHandler(inst, action, client, log)
	})
}

// resolveGiteaClient creates a Gitea API client with appropriate authentication.
// For OAuth2, the client uses a token-injecting HTTP transport that auto-refreshes.
// For static tokens, the client uses a standard token-based connection.
func resolveGiteaClient(inst config.GiteaInstance, log *zap.Logger) (*gitea.Client, error) {
	if inst.Auth != nil && inst.Auth.OAuth2 != nil {
		refresher, err := resolveOAuth2Refresher(inst.Auth.OAuth2, "gitea", inst.Name, log)
		if err != nil {
			return nil, err
		}
		return gitea.NewClientWithRefresher(refresher, inst.BaseURL, inst.InsecureSkipVerify, false, log)
	}

	var secretFile, secretKey string
	if inst.Auth != nil {
		secretFile = inst.Auth.SecretFile
		secretKey = inst.Auth.SecretKey
	}
	token, err := secrets.ResolveOrInfer(secretFile, "gitea", inst.Name, "token", secretKey, log)
	if err != nil {
		return nil, err
	}
	return gitea.NewClient(token, inst.BaseURL, inst.InsecureSkipVerify, false, log)
}

// buildHandler creates the appropriate handler based on action type.
func (f *GiteaFactory) buildHandler(inst config.GiteaInstance, action config.Action, client *gitea.Client, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case notifier.ActionCommitStatus:
		return gitea.NewStatusReporter(client, inst.Name, log)
	case notifier.ActionPRComment:
		return gitea.NewPRCommentHandler(gitea.PRCommentConfig{
			Client:   client,
			Name:     inst.Name,
			Template: action.Template,
			Mode:     action.Mode,
			Log:      log,
		})
	case notifier.ActionIssueComment:
		return gitea.NewIssueCommentHandler(gitea.IssueCommentConfig{
			Client:   client,
			Name:     inst.Name,
			Template: action.Template,
			Mode:     action.Mode,
			Log:      log,
		})
	case notifier.ActionLabel:
		return gitea.NewLabelHandler(gitea.LabelConfig{
			Client: client,
			Name:   inst.Name,
			Labels: labelSet(action),
			Log:    log,
		})
	default:
		return nil, ErrUnsupportedActionType
	}
}
