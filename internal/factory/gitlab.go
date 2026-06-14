package factory

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
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

	token, err := resolveGitLabToken(inst, log)
	if err != nil {
		return nil, err
	}

	return buildActionsWithMiddleware(inst.Actions, log, func(action config.Action) (notifier.ActionHandler, error) {
		return f.buildHandler(inst, action, token, log)
	})
}

// resolveGitLabToken resolves the authentication token for a GitLab instance.
// It supports both secret_file and OAuth2 client_credentials grant.
func resolveGitLabToken(inst config.GitLabInstance, log *zap.Logger) (string, error) {
	if inst.Auth == nil {
		return secrets.ResolveOrInfer("", "gitlab", inst.Name, "token", "", log)
	}
	if inst.Auth.OAuth2 != nil {
		return resolveOAuth2Token(inst.Auth.OAuth2, "gitlab", inst.Name, log)
	}
	return secrets.ResolveOrInfer(inst.Auth.SecretFile, "gitlab", inst.Name, "token", inst.Auth.SecretKey, log)
}

// resolveOAuth2Token fetches a token using the OAuth2 client_credentials grant.
func resolveOAuth2Token(oauth2cfg *config.OAuth2ClientCredentials, provider, name string, log *zap.Logger) (string, error) {
	clientID, err := secrets.ResolveOrInfer(oauth2cfg.ClientIDFile, provider, name, "client_id", oauth2cfg.ClientIDKey, log)
	if err != nil {
		return "", fmt.Errorf("oauth2 client_id: %w", err)
	}
	clientSecret, err := secrets.ResolveOrInfer(oauth2cfg.ClientSecretFile, provider, name, "client_secret", oauth2cfg.ClientSecretKey, log)
	if err != nil {
		return "", fmt.Errorf("oauth2 client_secret: %w", err)
	}
	client := scmoauth2.NewClient(scmoauth2.ClientCredentials{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     oauth2cfg.TokenURL,
	}, nil)
	tok, err := client.Token(context.Background())
	if err != nil {
		return "", fmt.Errorf("oauth2 token fetch: %w", err)
	}
	return tok, nil
}

// buildHandler creates the appropriate handler based on action type.
// Note: variant field is metadata-only. GitLab SaaS and self-managed
// use identical API protocols; variant documents deployment model and
// enables future SaaS-specific behaviors (rate limiting, feature flags).
func (f *GitLabFactory) buildHandler(inst config.GitLabInstance, action config.Action, token string, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case notifier.ActionCommitStatus:
		return gitlab.NewStatusReporter(token, inst.BaseURL, inst.Name, inst.InsecureSkipVerify, log)
	case notifier.ActionCommitComment:
		return gitlab.NewCommitCommentHandler(gitlab.CommitCommentConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Name:               inst.Name,
			Template:           action.Template,
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		})
	case notifier.ActionPRComment:
		return gitlab.NewMRCommentHandler(gitlab.MRCommentConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Name:               inst.Name,
			Template:           action.Template,
			Mode:               action.Mode,
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		})
	case notifier.ActionDeploymentStatus:
		return gitlab.NewDeploymentHandler(gitlab.DeploymentConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Name:               inst.Name,
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		})
	case notifier.ActionLabel:
		return gitlab.NewLabelHandler(gitlab.LabelConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Name:               inst.Name,
			Labels:             labelSet(action),
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		})
	default:
		return nil, ErrUnsupportedActionType
	}
}
