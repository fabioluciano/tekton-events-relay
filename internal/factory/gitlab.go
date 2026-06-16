package factory

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/gitlab"
	scmoauth2 "github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/oauth2"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// GitLabFactory builds ActionHandlers from GitLab instance configurations.
type GitLabFactory struct{}

// Build creates action handlers for a single GitLab instance.
func (f *GitLabFactory) Build(inst config.GitLabInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	client, err := resolveGitLabClient(inst, log)
	if err != nil {
		return nil, err
	}

	return buildActionsWithMiddleware(inst.Actions, log, func(action config.Action) (notifier.ActionHandler, error) {
		return f.buildHandler(inst, action, client, log)
	})
}

// resolveGitLabClient creates a GitLab API client with appropriate authentication.
// For OAuth2, the client uses an AuthSource that auto-refreshes tokens.
// For static tokens, the client uses a standard token-based connection.
func resolveGitLabClient(inst config.GitLabInstance, log *zap.Logger) (*gitlab.Client, error) {
	if inst.Auth != nil && inst.Auth.OAuth2 != nil {
		refresher, err := resolveOAuth2Refresher(inst.Auth.OAuth2, "gitlab", inst.Name, log)
		if err != nil {
			return nil, err
		}
		return gitlab.NewClientWithRefresher(refresher, inst.BaseURL, inst.InsecureSkipVerify, false, log)
	}

	var secretFile, secretKey string
	if inst.Auth != nil {
		secretFile = inst.Auth.SecretFile
		secretKey = inst.Auth.SecretKey
	}
	token, err := secrets.ResolveOrInfer(secretFile, "gitlab", inst.Name, "token", secretKey, log)
	if err != nil {
		return nil, err
	}
	return gitlab.NewClient(token, inst.BaseURL, inst.InsecureSkipVerify, false, log)
}

// resolveOAuth2Refresher creates an OAuth2 TokenRefresher from client_credentials config.
// The returned refresher auto-refreshes tokens via x/oauth2 TokenSource.
func resolveOAuth2Refresher(oauth2cfg *config.OAuth2ClientCredentials, provider, name string, log *zap.Logger) (scm.TokenRefresher, error) {
	clientID, err := secrets.ResolveOrInfer(oauth2cfg.ClientIDFile, provider, name, "client_id", oauth2cfg.ClientIDKey, log)
	if err != nil {
		return nil, fmt.Errorf("oauth2 client_id: %w", err)
	}
	clientSecret, err := secrets.ResolveOrInfer(oauth2cfg.ClientSecretFile, provider, name, "client_secret", oauth2cfg.ClientSecretKey, log)
	if err != nil {
		return nil, fmt.Errorf("oauth2 client_secret: %w", err)
	}
	return scmoauth2.NewClient(scmoauth2.ClientCredentials{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     oauth2cfg.TokenURL,
	}, nil), nil
}

// buildHandler creates the appropriate handler based on action type.
func (f *GitLabFactory) buildHandler(inst config.GitLabInstance, action config.Action, client *gitlab.Client, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case notifier.ActionCommitStatus:
		return gitlab.NewStatusReporter(client, inst.Name, log)
	case notifier.ActionCommitComment:
		return gitlab.NewCommitCommentHandler(gitlab.CommitCommentConfig{
			Client:   client,
			Name:     inst.Name,
			Template: action.Template,
			Log:      log,
		})
	case notifier.ActionPRComment:
		return gitlab.NewMRCommentHandler(gitlab.MRCommentConfig{
			Client:   client,
			Name:     inst.Name,
			Template: action.Template,
			Mode:     action.Mode,
			Log:      log,
		})
	case notifier.ActionDeploymentStatus:
		return gitlab.NewDeploymentHandler(gitlab.DeploymentConfig{
			Client: client,
			Name:   inst.Name,
			Log:    log,
		})
	case notifier.ActionLabel:
		return gitlab.NewLabelHandler(gitlab.LabelConfig{
			Client: client,
			Name:   inst.Name,
			Labels: labelSet(action),
			Log:    log,
		})
	default:
		return nil, ErrUnsupportedActionType
	}
}
