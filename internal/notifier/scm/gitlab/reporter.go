// Package gitlab implements the Reporter for GitLab Cloud and GitLab Server.
// API: POST /projects/{id|url-encoded-path}/statuses/{sha}
// Doc: https://docs.gitlab.com/ee/api/commits.html#post-the-build-status-of-a-commit
package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	nameGitLabCloud  = "gitlab-cloud"
	nameGitLabServer = "gitlab-server"
	gitLabAPIBaseURL = "https://gitlab.com/api/v4"
	statePending     = "pending"
	stateRunning     = "running"
	stateSuccess     = "success"
	stateFailed      = "failed"
	// JSON field names
	fieldState       = "state"
	fieldName        = "name"
	fieldDescription = "description"
	fieldTargetURL   = "target_url"
)

// Config holds the GitLab reporter configuration.
type Config struct {
	Name               string // "gitlab-cloud" or "gitlab-server"
	Token              string // Personal Access Token (api scope)
	BaseURL            string // default https://gitlab.com/api/v4
	InsecureSkipVerify bool   // Skip TLS verification for self-signed certificates
}

// Reporter sends commit status updates to GitLab.
type Reporter struct {
	base *notifier.Base
	cfg  Config
}

// New creates a new GitLab reporter with the given configuration.
// The reporter name and base URL are configured based on the Config values.
func New(cfg Config) *Reporter {
	r := &Reporter{cfg: cfg}
	r.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildPayload: r.payload,
		BuildURL:     r.url,
		Auth:         r.auth,
		UserAgent:    notifier.UserAgent,
	}
	return r
}

// NewCloud creates a new GitLab Cloud reporter with the given configuration.
func NewCloud(cfg Config) *Reporter {
	cfg.Name = nameGitLabCloud
	if cfg.BaseURL == "" {
		cfg.BaseURL = gitLabAPIBaseURL
	}
	return New(cfg)
}

// NewServer creates a new GitLab Server reporter with the given configuration.
func NewServer(cfg Config) *Reporter {
	cfg.Name = nameGitLabServer
	// BaseURL is required for server
	return New(cfg)
}

// Name returns the reporter identifier.
func (r *Reporter) Name() string { return r.cfg.Name }

// Notify sends the commit status update to GitLab.
func (r *Reporter) Notify(ctx context.Context, s domain.Event) error {
	return r.base.Send(ctx, s)
}

// GitLab uses specific states.
// Doc: https://docs.gitlab.com/ee/api/commits.html ("state" field)
var gitlabStateMapLegacy = scm.StateMap{
	domain.StatePending:  statePending,
	domain.StateRunning:  stateRunning,
	domain.StateSuccess:  stateSuccess,
	domain.StateFailure:  stateFailed,
	domain.StateError:    stateFailed,
	domain.StateCanceled: "canceled",
}

// projectIdentifier returns the numeric ID or the url-encoded path.
func projectIdentifier(s domain.Event) (string, error) {
	if s.Repo.ID != "" {
		return s.Repo.ID, nil
	}
	if s.Repo.Owner != "" && s.Repo.Name != "" {
		return url.PathEscape(s.Repo.Owner + "/" + s.Repo.Name), nil
	}
	return "", fmt.Errorf("gitlab requires repo.ID or repo.Owner+Name")
}

func (r *Reporter) url(s domain.Event) (string, error) {
	base := s.APIBaseURL
	if base == "" {
		base = r.cfg.BaseURL
	}
	if base == "" {
		return "", fmt.Errorf("gitlab requires APIBaseURL")
	}
	id, err := projectIdentifier(s)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/projects/%s/statuses/%s",
		strings.TrimRight(base, "/"), id, s.CommitSHA), nil
}

func (r *Reporter) payload(s domain.Event) (any, error) {
	p := map[string]string{
		fieldState:       gitlabStateMapLegacy.Map(s.State, statePending),
		fieldName:        s.Context,
		fieldDescription: s.Description,
	}
	if s.TargetURL != "" {
		p[fieldTargetURL] = s.TargetURL
	}
	return p, nil
}

func (r *Reporter) auth(req *http.Request) {
	req.Header.Set("PRIVATE-TOKEN", r.cfg.Token)
}
