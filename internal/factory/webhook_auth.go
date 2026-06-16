package factory

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/webhook"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// resolveAuthSecrets builds a ResolvedAuth whose credentials refresh per request.
//
// Token/Password/Secret are TokenRefreshers: OAuth2 uses an auto-refreshing
// client_credentials source, while bearer/apikey/basic/hmac re-read their
// mounted secret file on every request so rotation is picked up without a
// restart. Webhook auth secrets must be explicitly provided (no path inference
// without instance context); OAuth2 client_id/client_secret may be inferred.
func (f *WebhookFactory) resolveAuthSecrets(name string, auth *config.WebhookAuthConfig, log *zap.Logger) (*webhook.ResolvedAuth, error) {
	resolved := &webhook.ResolvedAuth{
		Type:   auth.Type,
		Header: auth.Header,
	}

	switch auth.Type {
	case "oauth2":
		refresher, err := resolveOAuth2Refresher(auth.OAuth2, "webhook", name, log)
		if err != nil {
			return nil, err
		}
		resolved.Token = refresher
	case "bearer", "apikey":
		refresher, err := explicitFileRefresher(auth.TokenFile, log)
		if err != nil {
			return nil, err
		}
		resolved.Token = refresher
	case "basic":
		username, err := secrets.Resolve(auth.UsernameFile, log)
		if err != nil {
			return nil, err
		}
		resolved.Username = username
		password, err := explicitFileRefresher(auth.PasswordFile, log)
		if err != nil {
			return nil, err
		}
		resolved.Password = password
	case "hmac":
		secret, err := explicitFileRefresher(auth.SecretFile, log)
		if err != nil {
			return nil, err
		}
		resolved.Secret = secret
	}

	return resolved, nil
}

// explicitFileRefresher validates an explicit secret path is readable and
// returns a source that re-reads it per request.
func explicitFileRefresher(path string, log *zap.Logger) (scm.TokenRefresher, error) {
	if path == "" {
		return nil, fmt.Errorf("webhook auth: missing required secret file path")
	}
	if _, err := secrets.Resolve(path, log); err != nil {
		return nil, err
	}
	return secrets.NewFileTokenSource(path), nil
}
