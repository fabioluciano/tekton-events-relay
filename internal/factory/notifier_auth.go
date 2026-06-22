package factory

import (
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

// resolveBearerRefresher builds a TokenRefresher for a notifier that authenticates
// with a single bearer-style token. When oauth2cfg is set it returns an
// auto-refreshing OAuth2 client_credentials source; otherwise it returns a
// file-backed source that re-reads the mounted secret on every request (so
// secret rotation is picked up without a pod restart). The secret is read once
// up front to fail fast on a missing/unreadable file.
func resolveBearerRefresher(oauth2cfg *config.OAuth2Config, tokenFile, tokenKey, provider, name string, log *zap.Logger) (scm.TokenRefresher, error) {
	if oauth2cfg != nil {
		return resolveOAuth2Refresher(oauth2cfg, provider, name, log)
	}
	return resolveFileRefresher(tokenFile, tokenKey, provider, name, log)
}

// resolveFileRefresher resolves a secret path (explicit or inferred), validates
// it is readable, and returns a source that re-reads it per request.
func resolveFileRefresher(explicitFile, key, provider, name string, log *zap.Logger) (scm.TokenRefresher, error) {
	path, err := secrets.InferPath(explicitFile, provider, name, "token", key)
	if err != nil {
		return nil, err
	}
	if _, err := secrets.Resolve(path, log); err != nil {
		return nil, err
	}
	return secrets.NewFileTokenSource(path), nil
}
