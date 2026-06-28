package factory

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/bitbucket"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// BitbucketFactory builds ActionHandlers from Bitbucket instance configurations.
type BitbucketFactory struct{}

// Build creates action handlers for a single Bitbucket instance.
func (f *BitbucketFactory) Build(inst config.BitbucketInstance, log *zap.Logger) ([]notifier.ActionHandler, error) {
	if !inst.Enabled {
		return nil, nil
	}

	if inst.Variant == config.BitbucketVariantCloud {
		client, username, appPassword, err := resolveCloudAuth(inst, log)
		if err != nil {
			return nil, err
		}
		return buildActionsWithMiddleware(inst.Actions, log, func(action config.Action) (notifier.ActionHandler, error) {
			return f.buildCloudHandler(inst, action, client, username, appPassword, log)
		})
	}

	token, err := resolveServerAuth(inst, log)
	if err != nil {
		return nil, err
	}
	return buildActionsWithMiddleware(inst.Actions, log, func(action config.Action) (notifier.ActionHandler, error) {
		return f.buildServerHandler(inst, action, token, log)
	})
}

// resolveCloudAuth resolves authentication for Bitbucket Cloud.
// Supports both basic auth (username_file + app_password_file) and OAuth2.
// For OAuth2, returns a pre-built CloudClient that resolves tokens per-request.
// For basic auth, returns username and appPassword strings.
func resolveCloudAuth(inst config.BitbucketInstance, log *zap.Logger) (client *bitbucket.CloudClient, username, appPassword string, err error) {
	if inst.Auth == nil {
		username, err = secrets.ResolveOrInfer("", "bitbucket", inst.Name, "username", "", log)
		if err != nil {
			return nil, "", "", err
		}
		appPassword, err = secrets.ResolveOrInfer("", "bitbucket", inst.Name, "app_password", "", log)
		return nil, username, appPassword, err
	}

	if inst.Auth.OAuth2 != nil {
		refresher, rErr := resolveOAuth2Refresher(inst.Auth.OAuth2, "bitbucket", inst.Name, log)
		if rErr != nil {
			return nil, "", "", fmt.Errorf("cloud oauth2: %w", rErr)
		}
		authFn := func(r *http.Request) {
			tok, tErr := refresher.Token(r.Context())
			if tErr != nil {
				log.Warn("bitbucket cloud oauth2 token refresh failed",
					zap.String("instance", inst.Name), zap.Error(tErr))
				return
			}
			cred := base64.StdEncoding.EncodeToString([]byte("x-token-auth:" + tok))
			r.Header.Set("Authorization", "Basic "+cred)
		}
		client = bitbucket.NewCloudClientWithAuth(authFn, inst.BaseURL, inst.InsecureSkipVerify, false, log)
		return client, "", "", nil
	}

	username, err = secrets.ResolveOrInfer(inst.Auth.UsernameFile, "bitbucket", inst.Name, "username", inst.Auth.UsernameKey, log)
	if err != nil {
		return nil, "", "", err
	}
	appPassword, err = secrets.ResolveOrInfer(inst.Auth.AppPasswordFile, "bitbucket", inst.Name, "app_password", inst.Auth.AppPasswordKey, log)
	return nil, username, appPassword, err
}

// resolveServerAuth resolves the token for Bitbucket Server.
func resolveServerAuth(inst config.BitbucketInstance, log *zap.Logger) (string, error) {
	if inst.Auth == nil {
		return secrets.ResolveOrInfer("", "bitbucket", inst.Name, "token", "", log)
	}
	return secrets.ResolveOrInfer(inst.Auth.TokenFile, "bitbucket", inst.Name, "token", inst.Auth.TokenKey, log)
}

// buildCloudHandler creates a Bitbucket Cloud handler for the given action.
func (f *BitbucketFactory) buildCloudHandler(inst config.BitbucketInstance, action config.Action, client *bitbucket.CloudClient, username, appPassword string, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case notifier.ActionCommitStatus:
		if client != nil {
			return bitbucket.NewCloudStatusReporterWithClient(client), nil
		}
		return bitbucket.NewCloudStatusReporter(username, appPassword, inst.BaseURL, inst.InsecureSkipVerify, log), nil
	case notifier.ActionPRComment:
		return bitbucket.NewCloudCommentHandler(bitbucket.CloudCommentConfig{
			Username:           username,
			AppPassword:        appPassword,
			BaseURL:            inst.BaseURL,
			Template:           action.Template,
			Mode:               action.Mode,
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
			Client:             client,
		})
	default:
		return nil, ErrUnsupportedActionType
	}
}

// buildServerHandler creates a Bitbucket Server handler for the given action.
func (f *BitbucketFactory) buildServerHandler(inst config.BitbucketInstance, action config.Action, token string, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case notifier.ActionCommitStatus:
		return bitbucket.NewServerStatusReporter(token, inst.BaseURL, inst.InsecureSkipVerify, log), nil
	case notifier.ActionPRComment:
		if action.Mode == "upsert" {
			log.Warn("comment mode 'upsert' is not supported on Bitbucket Server, using 'create'",
				zap.String("instance", inst.Name),
				zap.String("action", action.Name))
		}
		return bitbucket.NewServerCommentHandler(bitbucket.ServerCommentConfig{
			Token:              token,
			BaseURL:            inst.BaseURL,
			Template:           action.Template,
			InsecureSkipVerify: inst.InsecureSkipVerify,
			Log:                log,
		})
	default:
		return nil, ErrUnsupportedActionType
	}
}
