// Package azuredevops implements the Reporter for Azure Repos (Azure DevOps).
//
// API: POST /{org}/{project}/_apis/git/repositories/{repo}/commits/{sha}/statuses?api-version=7.1
// Doc: https://learn.microsoft.com/rest/api/azure/devops/git/statuses/create
package azuredevops

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// AzureStatus models the Azure DevOps status payload, with the nested
// "context" structure.
type AzureStatus struct {
	State       string       `json:"state"`
	Description string       `json:"description,omitempty"`
	TargetURL   string       `json:"targetUrl,omitempty"`
	Context     AzureContext `json:"context"`
}

// AzureContext represents the context field in Azure DevOps status.
type AzureContext struct {
	Name  string `json:"name"`
	Genre string `json:"genre,omitempty"`
}

// Config holds Azure DevOps reporter configuration.
type Config struct {
	Token              string // PAT with scope vso.code_status
	BaseURL            string // default https://dev.azure.com
	Genre              string // default "tekton-ci"
	InsecureSkipVerify bool   // Skip TLS certificate verification
}

// Reporter reports pipeline status to Azure DevOps.
type Reporter struct {
	base *notifier.Base
	cfg  Config
}

// New creates a new Azure DevOps reporter.
func New(cfg Config) *Reporter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://dev.azure.com"
	}
	if cfg.Genre == "" {
		cfg.Genre = "tekton-ci"
	}
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

// Name returns the reporter identifier.
func (r *Reporter) Name() string { return "azure-devops" }

// Notify sends the event to Azure DevOps.
func (r *Reporter) Notify(ctx context.Context, s domain.Event) error {
	return r.base.Send(ctx, s)
}

// Estados Azure: notSet, pending, succeeded, failed, error, notApplicable
// Doc: GitStatusState enum
var azureStateMapLegacy = scm.StateMap{
	domain.StatePending:  "pending",
	domain.StateRunning:  "pending",
	domain.StateSuccess:  "succeeded",
	domain.StateFailure:  "failed",
	domain.StateError:    "error",
	domain.StateCanceled: "error",
}

func (r *Reporter) url(s domain.Event) (string, error) {
	base := s.APIBaseURL
	if base == "" {
		base = r.cfg.BaseURL
	}
	if base == "" {
		return "", fmt.Errorf("azure-devops requires APIBaseURL")
	}
	if s.Repo.Org == "" || s.Repo.Project == "" || s.Repo.Name == "" {
		return "", fmt.Errorf("azure-devops requires org, project and repo name")
	}
	return fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/commits/%s/statuses?api-version=7.1",
		strings.TrimRight(base, "/"),
		s.Repo.Org, s.Repo.Project, s.Repo.Name, s.CommitSHA), nil
}

func (r *Reporter) payload(s domain.Event) (any, error) {
	if len(s.Description) > 4000 {
		return nil, fmt.Errorf("field %q exceeds limit (4000 chars, got %d)", "description", len(s.Description))
	}
	return AzureStatus{
		State:       azureStateMapLegacy.Map(s.State, "pending"),
		Description: s.Description,
		TargetURL:   s.TargetURL,
		Context: AzureContext{
			Name:  s.Context,
			Genre: r.cfg.Genre,
		},
	}, nil
}

// Auth Azure: Basic with empty username and PAT as password.
func (r *Reporter) auth(req *http.Request) {
	req.SetBasicAuth("", r.cfg.Token)
}
