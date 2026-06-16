package webhook

import "github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"

// ResolvedAuth holds resolved credential providers, never file paths.
// Populated by the factory; consumed by auth.go. Token/Password/Secret are
// TokenRefreshers so credentials are resolved fresh per request (mounted-secret
// re-read or OAuth2 refresh) and never go stale.
type ResolvedAuth struct {
	Type     string
	Token    scm.TokenRefresher // bearer/apikey token or oauth2 access token
	Username string             // resolved basic auth username
	Password scm.TokenRefresher // basic auth password
	Secret   scm.TokenRefresher // HMAC secret
	Header   string             // API key header name
}
