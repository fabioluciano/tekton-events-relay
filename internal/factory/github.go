package factory

import (
	"golang.org/x/time/rate"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
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
		appClient, err := github.NewAppClient(inst.Auth.AppID, inst.Auth.InstallationID, inst.BaseURL, inst.InsecureSkipVerify, log, inst.Auth.PrivateKeyFile, nil)
		if err != nil {
			return nil, err
		}
		client = github.NewClientWithRefresher(appClient, inst.BaseURL, inst.InsecureSkipVerify, log, false)
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

	handlers, err := buildActionsWithMiddleware(inst.Actions, log, func(action config.Action) (notifier.ActionHandler, error) {
		return f.buildHandler(inst, action, client, log)
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
func (f *GitHubFactory) buildHandler(inst config.GitHubInstance, action config.Action, client github.HTTPDoer, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case notifier.ActionCommitStatus:
		return github.NewStatusReporter(client, inst.Name, log), nil
	case notifier.ActionCommitComment:
		return github.NewCommitCommentHandler(github.CommitCommentConfig{
			Client:   client,
			Name:     inst.Name,
			Template: action.Template,
		}, log)
	case notifier.ActionPRComment:
		return github.NewPRCommentHandler(github.PRCommentConfig{
			Client:   client,
			Name:     inst.Name,
			Template: action.Template,
			Mode:     action.Mode,
		}, log)
	case notifier.ActionIssueComment:
		return github.NewIssueCommentHandler(github.IssueCommentConfig{
			Client:   client,
			Name:     inst.Name,
			Template: action.Template,
			Mode:     action.Mode,
		}, log)
	case notifier.ActionDiscussionComment:
		return github.NewDiscussionCommentHandler(github.DiscussionCommentConfig{
			Client:   client,
			Name:     inst.Name,
			Template: action.Template,
		}, log)
	case notifier.ActionLabel:
		return github.NewLabelHandler(github.LabelConfig{
			Client: client,
			Name:   inst.Name,
			Labels: labelSet(action),
		}, log), nil
	case notifier.ActionCheckRun:
		return github.NewCheckRunHandler(github.CheckRunConfig{
			Client:   client,
			InstName: inst.Name,
			Name:     action.Name,
			Template: action.Template,
		}, log)
	case notifier.ActionDeploymentStatus:
		return github.NewDeploymentStatusHandler(github.DeploymentStatusConfig{
			Client: client,
			Name:   inst.Name,
		}, log), nil
	default:
		return nil, ErrUnsupportedActionType
	}
}
