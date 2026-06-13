package factory

import (
	"fmt"

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

	// Resolve secrets from volume mounts based on variant
	var username, appPassword, token string
	var err error

	if inst.Variant == config.BitbucketVariantCloud {
		username, appPassword, err = resolveCloudAuth(inst, log)
		if err != nil {
			return nil, err
		}
	} else {
		token, err = resolveServerAuth(inst, log)
		if err != nil {
			return nil, err
		}
	}

	return buildActionsWithMiddleware(inst.Actions, log, func(action config.Action) (notifier.ActionHandler, error) {
		return f.buildHandler(inst, action, username, appPassword, token, log)
	})
}

// resolveCloudAuth resolves username and app_password for Bitbucket Cloud.
// It supports both basic auth (username_file + app_password_file) and OAuth2.
func resolveCloudAuth(inst config.BitbucketInstance, log *zap.Logger) (username, appPassword string, err error) {
	if inst.Auth == nil {
		username, err = secrets.ResolveOrInfer("", "bitbucket", inst.Name, "username", "", log)
		if err != nil {
			return "", "", err
		}
		appPassword, err = secrets.ResolveOrInfer("", "bitbucket", inst.Name, "app_password", "", log)
		return username, appPassword, err
	}

	if inst.Auth.OAuth2 != nil {
		tok, err := resolveOAuth2Token(inst.Auth.OAuth2, "bitbucket", inst.Name, log)
		if err != nil {
			return "", "", fmt.Errorf("cloud oauth2: %w", err)
		}
		// For OAuth2 on Bitbucket Cloud, use the token as a Bearer token via appPassword field
		// and empty username (Bitbucket Cloud supports x-token-auth scheme).
		return "x-token-auth", tok, nil
	}

	username, err = secrets.ResolveOrInfer(inst.Auth.UsernameFile, "bitbucket", inst.Name, "username", inst.Auth.UsernameKey, log)
	if err != nil {
		return "", "", err
	}
	appPassword, err = secrets.ResolveOrInfer(inst.Auth.AppPasswordFile, "bitbucket", inst.Name, "app_password", inst.Auth.AppPasswordKey, log)
	return username, appPassword, err
}

// resolveServerAuth resolves the token for Bitbucket Server.
func resolveServerAuth(inst config.BitbucketInstance, log *zap.Logger) (string, error) {
	if inst.Auth == nil {
		return secrets.ResolveOrInfer("", "bitbucket", inst.Name, "token", "", log)
	}
	return secrets.ResolveOrInfer(inst.Auth.TokenFile, "bitbucket", inst.Name, "token", inst.Auth.TokenKey, log)
}

// buildHandler creates the appropriate handler based on action type and variant.
func (f *BitbucketFactory) buildHandler(inst config.BitbucketInstance, action config.Action, username, appPassword, token string, log *zap.Logger) (notifier.ActionHandler, error) {
	switch action.Type {
	case notifier.ActionCommitStatus:
		if inst.Variant == config.BitbucketVariantCloud {
			return bitbucket.NewCloudStatusReporter(username, appPassword, inst.BaseURL, inst.InsecureSkipVerify, log), nil
		}
		return bitbucket.NewServerStatusReporter(token, inst.BaseURL, inst.InsecureSkipVerify, log), nil
	case notifier.ActionPRComment:
		if inst.Variant == config.BitbucketVariantCloud {
			return bitbucket.NewCloudCommentHandler(bitbucket.CloudCommentConfig{
				Username:           username,
				AppPassword:        appPassword,
				BaseURL:            inst.BaseURL,
				Template:           action.Template,
				Mode:               action.Mode,
				InsecureSkipVerify: inst.InsecureSkipVerify,
				Log:                log,
			})
		}
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
