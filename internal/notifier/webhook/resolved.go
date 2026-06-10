package webhook

// ResolvedAuth holds only resolved credential values, never file paths.
// Populated by the factory after reading secret files; consumed by auth.go.
type ResolvedAuth struct {
	Type     string
	Token    string // resolved bearer token or API key value
	Username string // resolved basic auth username
	Password string // resolved basic auth password
	Secret   string // resolved HMAC secret
	Header   string // API key header name
}
