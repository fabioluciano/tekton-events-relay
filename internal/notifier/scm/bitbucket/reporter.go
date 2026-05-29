// Package bitbucket implements Reporters for Bitbucket Cloud and Bitbucket
// Server (Data Center). The APIs are quite different — hence two reporters.
//
// Cloud:  POST /2.0/repositories/{workspace}/{repo}/commit/{sha}/statuses/build
// Server: POST /rest/build-status/1.0/commits/{sha}
package bitbucket

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	stateSuccessful     = "SUCCESSFUL"
	stateFailed         = "FAILED"
	stateInProgress     = "INPROGRESS"
	bitbucketAPIBaseURL = "https://api.bitbucket.org"
)

// ============================================================
//   Bitbucket Cloud
// ============================================================

// CloudConfig holds configuration for Bitbucket Cloud reporter.
type CloudConfig struct {
	Username           string // Bitbucket user
	AppPassword        string // App password with repository:write scope
	BaseURL            string // default https://api.bitbucket.org
	InsecureSkipVerify bool   // Skip TLS certificate verification
}

// CloudReporter reports pipeline status to Bitbucket Cloud.
type CloudReporter struct {
	base *notifier.Base
	cfg  CloudConfig
}

// NewCloud creates a new Bitbucket Cloud reporter with the given configuration.
func NewCloud(cfg CloudConfig) *CloudReporter {
	r := &CloudReporter{cfg: cfg}
	r.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildPayload: r.payload,
		BuildURL:     r.url,
		Auth:         r.auth,
		UserAgent:    notifier.UserAgent,
	}
	return r
}

// Name returns the notifier identifier for Bitbucket Cloud.
func (r *CloudReporter) Name() string { return "bitbucket-cloud" }

// Notify sends a build status update to Bitbucket Cloud.
func (r *CloudReporter) Notify(ctx context.Context, s domain.Event) error {
	return r.base.Send(ctx, s)
}

var bitbucketCloudStateMapLegacy = scm.StateMap{
	domain.StatePending:  stateInProgress,
	domain.StateRunning:  stateInProgress,
	domain.StateSuccess:  stateSuccessful,
	domain.StateFailure:  stateFailed,
	domain.StateError:    stateFailed,
	domain.StateCanceled: "STOPPED",
}

func (r *CloudReporter) url(s domain.Event) (string, error) {
	base := s.APIBaseURL
	if base == "" {
		base = r.cfg.BaseURL
	}
	if base == "" {
		base = bitbucketAPIBaseURL
	}
	ws := s.Repo.Workspace
	if ws == "" {
		ws = s.Repo.Owner
	}
	if ws == "" || s.Repo.Name == "" {
		return "", fmt.Errorf("bitbucket-cloud requires Workspace and Name")
	}
	return fmt.Sprintf("%s/2.0/repositories/%s/%s/commit/%s/statuses/build",
		strings.TrimRight(base, "/"), ws, s.Repo.Name, s.CommitSHA), nil
}

func buildKey(s domain.Event) string {
	if s.Context != "" {
		return s.Context
	}
	return "tekton-" + s.RunName
}

func validatePayloadFields(key, context, description string) error {
	if len(key) > 40 {
		return fmt.Errorf("field %q exceeds limit (40 chars, got %d)", "key", len(key))
	}
	if len(context) > 255 {
		return fmt.Errorf("field %q exceeds limit (255 chars, got %d)", "name", len(context))
	}
	if len(description) > 255 {
		return fmt.Errorf("field %q exceeds limit (255 chars, got %d)", "description", len(description))
	}
	return nil
}

func (r *CloudReporter) payload(s domain.Event) (any, error) {
	key := buildKey(s)
	if err := validatePayloadFields(key, s.Context, s.Description); err != nil {
		return nil, err
	}
	return map[string]string{
		"key":         key,
		"state":       bitbucketCloudStateMapLegacy.Map(s.State, stateInProgress),
		"name":        s.Context,
		"description": s.Description,
		"url":         s.TargetURL,
	}, nil
}

func (r *CloudReporter) auth(req *http.Request) {
	cred := r.cfg.Username + ":" + r.cfg.AppPassword
	req.Header.Set("Authorization",
		"Basic "+base64.StdEncoding.EncodeToString([]byte(cred)))
}

// ============================================================
//   Bitbucket Server (Data Center)
// ============================================================

// ServerConfig holds configuration for Bitbucket Server reporter.
type ServerConfig struct {
	Token              string // Personal access token
	BaseURL            string // ex: https://bitbucket.company.com
	InsecureSkipVerify bool   // Skip TLS certificate verification
}

// ServerReporter reports pipeline status to Bitbucket Server (Data Center).
type ServerReporter struct {
	base *notifier.Base
	cfg  ServerConfig
}

// NewServer creates a new Bitbucket Server reporter with the given configuration.
func NewServer(cfg ServerConfig) *ServerReporter {
	r := &ServerReporter{cfg: cfg}
	r.base = &notifier.Base{
		HTTP:         notifier.DefaultHTTPClient(),
		BuildPayload: r.payload,
		BuildURL:     r.url,
		Auth:         r.auth,
		UserAgent:    notifier.UserAgent,
	}
	return r
}

// Name returns the notifier identifier for Bitbucket Server.
func (r *ServerReporter) Name() string { return "bitbucket-server" }

// Notify sends a build status update to Bitbucket Server.
func (r *ServerReporter) Notify(ctx context.Context, s domain.Event) error {
	return r.base.Send(ctx, s)
}

var bitbucketServerStateMapLegacy = scm.StateMap{
	domain.StatePending:  "INPROGRESS",
	domain.StateRunning:  "INPROGRESS",
	domain.StateSuccess:  stateSuccessful,
	domain.StateFailure:  stateFailed,
	domain.StateError:    stateFailed,
	domain.StateCanceled: stateFailed,
}

func (r *ServerReporter) url(s domain.Event) (string, error) {
	base := s.APIBaseURL
	if base == "" {
		base = r.cfg.BaseURL
	}
	if base == "" {
		return "", fmt.Errorf("bitbucket-server requires APIBaseURL")
	}
	return fmt.Sprintf("%s/rest/build-status/1.0/commits/%s",
		strings.TrimRight(base, "/"), s.CommitSHA), nil
}

func (r *ServerReporter) payload(s domain.Event) (any, error) {
	key := buildKey(s)
	if err := validatePayloadFields(key, s.Context, s.Description); err != nil {
		return nil, err
	}
	if s.Repo.Project != "" && len(s.Repo.Project) > 255 {
		return nil, fmt.Errorf("field %q exceeds limit (255 chars, got %d)", "parent", len(s.Repo.Project))
	}
	payload := map[string]string{
		"state":       bitbucketServerStateMapLegacy.Map(s.State, "INPROGRESS"),
		"key":         key,
		"name":        s.Context,
		"url":         s.TargetURL,
		"description": s.Description,
	}
	if s.Repo.Project != "" {
		payload["parent"] = s.Repo.Project
	}
	return payload, nil
}

func (r *ServerReporter) auth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+r.cfg.Token)
}
