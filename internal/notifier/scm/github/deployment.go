package github

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const defaultEnvironment = "production"

// DeploymentStatusConfig holds GitHub Deployment Status handler configuration.
type DeploymentStatusConfig struct {
	Token              string
	BaseURL            string
	InsecureSkipVerify bool
}

// DeploymentStatusHandler reports deployment status to GitHub Deployments API.
// Requires a GitHub token with deployments:write permission.
// Uses annotations: tekton.dev/environment, tekton.dev/deployment-ref
type DeploymentStatusHandler struct {
	client *Client
	log    *zap.Logger
}

// NewDeploymentStatusHandler creates a Deployment Status handler.
func NewDeploymentStatusHandler(cfg DeploymentStatusConfig, log *zap.Logger) notifier.ActionHandler {
	return &DeploymentStatusHandler{
		client: NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, log, false),
		log:    log,
	}
}

// Name returns the provider identifier.
func (h *DeploymentStatusHandler) Name() string { return providerGitHub }

// Type returns the action type.
func (h *DeploymentStatusHandler) Type() notifier.ActionType {
	return notifier.ActionDeploymentStatus
}

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

	// Map state to deployment status
	state := h.mapState(e.State)

	// Step 1: Create deployment (only if not already created)
	// For stateless operation, we create deployment + status together
	// GitHub allows multiple statuses per deployment, so this is idempotent-ish
	deploymentPayload := map[string]any{
		"ref":                    e.CommitSHA,
		"environment":            environment,
		"auto_merge":             false,
		"required_contexts":      []string{},
		"transient_environment":  false,
		"production_environment": environment == defaultEnvironment,
		"description":            fmt.Sprintf("Pipeline: %s", e.RunName),
	}

	deploymentURL := fmt.Sprintf("%s/repos/%s/%s/deployments",
		h.client.baseURL, e.Repo.Owner, e.Repo.Name)

	var deploymentResp struct {
		ID int64 `json:"id"`
	}

	if err := h.client.DoWithResponse(ctx, "POST", deploymentURL, deploymentPayload, &deploymentResp); err != nil {
		return fmt.Errorf("create deployment: %w", err)
	}

	// Step 2: Create deployment status
	statusPayload := map[string]any{
		"state":       state,
		"description": e.Description,
	}

	if e.TargetURL != "" {
		statusPayload["log_url"] = e.TargetURL
		statusPayload["environment_url"] = e.TargetURL
	}

	statusURL := fmt.Sprintf("%s/repos/%s/%s/deployments/%d/statuses",
		h.client.baseURL, e.Repo.Owner, e.Repo.Name, deploymentResp.ID)

	return h.client.Do(ctx, "POST", statusURL, statusPayload)
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
