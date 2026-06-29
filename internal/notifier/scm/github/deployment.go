package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	gh "github.com/google/go-github/v68/github"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const defaultEnvironment = "production"

// DeploymentStatusConfig holds GitHub Deployment Status handler configuration.
type DeploymentStatusConfig struct {
	Client HTTPDoer
}

// DeploymentStatusHandler reports deployment status to GitHub Deployments API.
// Requires a GitHub token with deployments:write permission.
// Uses annotations: tekton.dev/environment, tekton.dev/deployment-ref
type DeploymentStatusHandler struct {
	client HTTPDoer
	log    *zap.Logger
}

// NewDeploymentStatusHandler creates a Deployment Status handler.
func NewDeploymentStatusHandler(cfg DeploymentStatusConfig, log *zap.Logger) notifier.ActionHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &DeploymentStatusHandler{
		client: cfg.Client,
		log:    log,
	}
}

// Name returns the provider identifier.
func (h *DeploymentStatusHandler) Name() string { return providerGitHub }

// Type returns the action type.
func (h *DeploymentStatusHandler) Type() notifier.ActionType {
	return notifier.ActionDeploymentStatus
}

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *DeploymentStatusHandler) Close() error { return nil }

// Handle creates a deployment and deployment status for the event.
// Two-step process:
// 1. POST /repos/{owner}/{repo}/deployments (create deployment)
// 2. POST /repos/{owner}/{repo}/deployments/{id}/statuses (create status)
func (h *DeploymentStatusHandler) Handle(ctx context.Context, e domain.Event) error {
	// Provider-match guard
	if e.Provider != providerGitHub {
		h.log.Debug("deployment status skipped: provider mismatch",
			zap.String("provider", e.Provider),
			zap.String("namespace", e.Namespace),
			zap.String("taskrun", e.RunName))
		return nil
	}

	// Validate required fields
	if e.CommitSHA == "" {
		h.log.Info("deployment status NOT sent: missing scm.commit-sha annotation",
			zap.String("namespace", e.Namespace),
			zap.String("taskrun", e.RunName))
		return nil
	}

	if e.Repo.Owner == "" || e.Repo.Name == "" {
		h.log.Warn("deployment status NOT sent: missing scm.repo-owner or scm.repo-name annotation",
			zap.String("namespace", e.Namespace),
			zap.String("taskrun", e.RunName))
		return nil
	}

	// Environment name from annotation (required for deployment)
	environment := e.Context
	if environment == "" {
		environment = defaultEnvironment
	}

	// Step 1: Create deployment.
	// GitHub allows multiple statuses per deployment, so a 409 on create means a
	// deployment for this ref/environment already exists — look it up and attach
	// the status to the existing deployment instead.
	req := &gh.DeploymentRequest{
		Ref:                   gh.Ptr(e.CommitSHA),
		Environment:           gh.Ptr(environment),
		AutoMerge:             gh.Ptr(false),
		RequiredContexts:      &[]string{},
		TransientEnvironment:  gh.Ptr(false),
		ProductionEnvironment: gh.Ptr(environment == defaultEnvironment),
		Description:           gh.Ptr(fmt.Sprintf("Pipeline: %s", e.RunName)),
	}

	deployment, _, err := h.client.GH().Repositories.CreateDeployment(ctx, e.Repo.Owner, e.Repo.Name, req)
	if err != nil {
		var ghErr *gh.ErrorResponse
		if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusConflict {
			existingID, findErr := h.findExistingDeployment(ctx, e.Repo.Owner, e.Repo.Name, e.CommitSHA, environment)
			if findErr != nil {
				return fmt.Errorf("create deployment conflict, lookup failed: %w", findErr)
			}
			return h.createDeploymentStatus(ctx, e.Repo.Owner, e.Repo.Name, existingID, e)
		}
		return fmt.Errorf("create deployment: %w", err)
	}

	// Step 2: Create deployment status.
	return h.createDeploymentStatus(ctx, e.Repo.Owner, e.Repo.Name, deployment.GetID(), e)
}

// createDeploymentStatus creates a deployment status for the given deployment ID.
func (h *DeploymentStatusHandler) createDeploymentStatus(ctx context.Context, owner, repo string, deploymentID int64, e domain.Event) error {
	req := &gh.DeploymentStatusRequest{
		State:       gh.Ptr(h.mapState(e.State)),
		Description: gh.Ptr(e.Description),
	}
	if e.TargetURL != "" {
		req.LogURL = gh.Ptr(e.TargetURL)
		req.EnvironmentURL = gh.Ptr(e.TargetURL)
	}
	_, _, err := h.client.GH().Repositories.CreateDeploymentStatus(ctx, owner, repo, deploymentID, req)
	return err
}

// mapState converts domain.State to GitHub deployment status state.
// GitHub deployment states: pending, success, failure, error, inactive, in_progress, queued
func (h *DeploymentStatusHandler) mapState(state domain.State) string {
	switch state {
	case domain.StatePending:
		return statusQueued
	case domain.StateRunning:
		return statusInProgress
	case domain.StateSuccess:
		return stateSuccess
	case domain.StateFailure:
		return stateFailure
	case domain.StateError:
		return stateError
	case domain.StateCanceled:
		return stateInactive
	default:
		return statusQueued
	}
}

// findExistingDeployment lists deployments and returns the ID of the first match.
func (h *DeploymentStatusHandler) findExistingDeployment(ctx context.Context, owner, repo, sha, environment string) (int64, error) {
	opts := &gh.DeploymentsListOptions{Ref: sha, Environment: environment}
	deployments, _, err := h.client.GH().Repositories.ListDeployments(ctx, owner, repo, opts)
	if err != nil {
		return 0, fmt.Errorf("list deployments: %w", err)
	}
	if len(deployments) == 0 {
		return 0, fmt.Errorf("no existing deployment found for ref=%s environment=%s", sha, environment)
	}
	return deployments[0].GetID(), nil
}
