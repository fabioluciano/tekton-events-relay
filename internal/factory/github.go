package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/github"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// GitHubFactory builds ActionHandlers from GitHub instance configurations.
type GitHubFactory struct{}

// Build creates action handlers for a single GitHub instance.
func (f *GitHubFactory) Build(inst config.GitHubInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	// Create HTTP client (strategy pattern: Client or AppClient)
	var client github.HTTPDoer
	if inst.Auth != nil && inst.Auth.AppID != 0 && inst.Auth.InstallationID != 0 {
		// GitHub App authentication with auto-refresh (reads private key from /etc/github-app/private-key.pem)
		appClient, err := github.NewAppClient(inst.Auth.AppID, inst.Auth.InstallationID, inst.BaseURL, inst.InsecureSkipVerify, log, inst.Auth.PrivateKeyFile)
		if err != nil {
			return nil, err
		}
		client = appClient
		log.Info("using GitHub App authentication",
			zap.Int64("app_id", inst.Auth.AppID),
			zap.Int64("installation_id", inst.Auth.InstallationID))
	} else {
		// Token-based authentication - resolve secret from volume mount
		var secretFile, secretKey string
		if inst.Auth != nil {
			secretFile = inst.Auth.SecretFile
			secretKey = inst.Auth.SecretKey
		}
		token, err := secrets.ResolveOrInfer(secretFile, "github", inst.Name, "token", secretKey, log)
		if err != nil {
			return nil, err
		}
		client = github.NewClient(token, inst.BaseURL, inst.InsecureSkipVerify, log, false)
	}

	return buildActionsWithMiddleware(inst.Actions, log, func(action config.Action) (notifier.ActionHandler, error) {
		return f.buildHandler(inst, action, client, log)
	})
}

// buildHandler creates the appropriate handler based on action type.
func (f *GitHubFactory) buildHandler(inst config.GitHubInstance, action config.Action, client github.HTTPDoer, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case notifier.ActionCommitStatus:
		return github.NewStatusReporter(client, log), nil
	case notifier.ActionCommitComment:
		return github.NewCommitCommentHandler(github.CommitCommentConfig{
			Token:              client.Token(),
			BaseURL:            client.BaseURL(),
			Template:           action.Template,
			InsecureSkipVerify: inst.InsecureSkipVerify,
		}, log)
	case notifier.ActionPRComment:
		return github.NewPRCommentHandler(github.PRCommentConfig{
			Token:              client.Token(),
			BaseURL:            client.BaseURL(),
			Template:           action.Template,
			Mode:               action.Mode,
			InsecureSkipVerify: inst.InsecureSkipVerify,
		}, log)
	case notifier.ActionIssueComment:
		return github.NewIssueCommentHandler(github.IssueCommentConfig{
			Token:              client.Token(),
			BaseURL:            client.BaseURL(),
			Template:           action.Template,
			Mode:               action.Mode,
			InsecureSkipVerify: inst.InsecureSkipVerify,
		}, log)
	case notifier.ActionDiscussionComment:
		return github.NewDiscussionCommentHandler(github.DiscussionCommentConfig{
			Token:              client.Token(),
			BaseURL:            client.BaseURL(),
			Template:           action.Template,
			InsecureSkipVerify: inst.InsecureSkipVerify,
		}, log)
	case notifier.ActionLabel:
		return github.NewLabelHandler(github.LabelConfig{
			Token:              client.Token(),
			BaseURL:            client.BaseURL(),
			Labels:             labelSet(action),
			InsecureSkipVerify: inst.InsecureSkipVerify,
		}, log), nil
	case notifier.ActionCheckRun:
		return github.NewCheckRunHandler(github.CheckRunConfig{
			Token:              client.Token(),
			BaseURL:            client.BaseURL(),
			Name:               action.Name,
			Template:           action.Template,
			InsecureSkipVerify: inst.InsecureSkipVerify,
		}, log)
	case notifier.ActionDeploymentStatus:
		return github.NewDeploymentStatusHandler(github.DeploymentStatusConfig{
			Token:              client.Token(),
			BaseURL:            client.BaseURL(),
			InsecureSkipVerify: inst.InsecureSkipVerify,
		}, log), nil
	default:
		return nil, ErrUnsupportedActionType
	}
}
