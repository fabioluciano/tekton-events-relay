package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/webhook"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// resolveAuthSecrets resolves all secret file references in webhook auth config
// and returns a ResolvedAuth containing only the resolved credential values.
// For webhook auth, paths cannot be inferred (no instance name), so we only resolve explicit paths.
func (f *WebhookFactory) resolveAuthSecrets(auth *config.WebhookAuthConfig, log *zap.Logger) (*webhook.ResolvedAuth, error) {
	resolved := &webhook.ResolvedAuth{
		Type:   auth.Type,
		Header: auth.Header,
	}

	var err error

	// Auth secrets must be explicitly provided (no inference without instance context)
	if auth.TokenFile != "" {
		resolved.Token, err = secrets.Resolve(auth.TokenFile, log)
		if err != nil {
			return nil, err
		}
	}

	if auth.UsernameFile != "" {
		resolved.Username, err = secrets.Resolve(auth.UsernameFile, log)
		if err != nil {
			return nil, err
		}
	}

	if auth.PasswordFile != "" {
		resolved.Password, err = secrets.Resolve(auth.PasswordFile, log)
		if err != nil {
			return nil, err
		}
	}

	if auth.SecretFile != "" {
		resolved.Secret, err = secrets.Resolve(auth.SecretFile, log)
		if err != nil {
			return nil, err
		}
	}

	return resolved, nil
}
