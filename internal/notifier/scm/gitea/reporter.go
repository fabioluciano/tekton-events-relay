// Package gitea implements the Reporter for Gitea/Forgejo.
// API: POST /api/v1/repos/{owner}/{repo}/statuses/{sha}
// Doc: https://docs.gitea.com/api/1.21/#tag/repository/operation/repoCreateStatus
//
// It is practically a clone of the GitHub API, but with base path /api/v1.
package gitea

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// Config holds the configuration for the Gitea reporter.
type Config struct {
	Token              string
	BaseURL            string // ex: https://gitea.company.com
	InsecureSkipVerify bool
}

// Reporter implements the notifier for Gitea/Forgejo commit status API.
type Reporter struct {
	base *notifier.Base
	cfg  Config
}

// New creates a new Gitea reporter with the given configuration.
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

// Name returns the identifier for this notifier.
func (r *Reporter) Name() string { return "gitea" }

// Notify sends the commit status to Gitea.
func (r *Reporter) Notify(ctx context.Context, s domain.Event) error {
	return r.base.Send(ctx, s)
}

// Gitea accepts: pending, success, error, failure, warning.
var giteaStateMapLegacy = scm.StateMap{
	domain.StatePending:  "pending",
	domain.StateRunning:  "pending",
	domain.StateSuccess:  "success",
	domain.StateFailure:  "failure",
	domain.StateError:    "error",
	domain.StateCanceled: "error",
}

func (r *Reporter) url(s domain.Event) (string, error) {
	base := s.APIBaseURL
	if base == "" {
		base = r.cfg.BaseURL
	}
	if base == "" {
		return "", fmt.Errorf("gitea requires APIBaseURL")
	}
	if s.Repo.Owner == "" || s.Repo.Name == "" {
		return "", fmt.Errorf("gitea requires owner and name")
	}
	return fmt.Sprintf("%s/api/v1/repos/%s/%s/statuses/%s",
		strings.TrimRight(base, "/"),
		s.Repo.Owner, s.Repo.Name, s.CommitSHA), nil
}

func (r *Reporter) payload(s domain.Event) (any, error) {
	return map[string]string{
		"state":       giteaStateMapLegacy.Map(s.State, "pending"),
		"context":     s.Context,
		"description": s.Description,
		"target_url":  s.TargetURL,
	}, nil
}

func (r *Reporter) auth(req *http.Request) {
	req.Header.Set("Authorization", "token "+r.cfg.Token)
}
