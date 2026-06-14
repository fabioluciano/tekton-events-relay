package gitlab

import (
	"context"

	gl "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const defaultEnvironment = "production"

// DeploymentHandler records deployments on GitLab's Environments page,
// giving parity with the GitHub deployment_status action. The environment
// name comes from the event context (annotation), defaulting to production.
type DeploymentHandler struct {
	client *Client
	name   string
	log    *zap.Logger
}

// DeploymentConfig configures the deployment handler.
type DeploymentConfig struct {
	Token              string
	BaseURL            string
	Name               string
	InsecureSkipVerify bool
	Log                *zap.Logger
}

// NewDeploymentHandler creates a new GitLab deployment handler.
func NewDeploymentHandler(cfg DeploymentConfig) (notifier.ActionHandler, error) {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	c, err := NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, false, cfg.Log)
	if err != nil {
		return nil, err
	}
	return &DeploymentHandler{
		client: c,
		name:   cfg.Name,
		log:    log,
	}, nil
}

// Name returns the handler name.
func (h *DeploymentHandler) Name() string { return h.name }

// Type returns the action type.
func (h *DeploymentHandler) Type() notifier.ActionType { return notifier.ActionDeploymentStatus }

// Handle records the run as a deployment to the environment.
func (h *DeploymentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != h.name {
		return nil
	}
	if e.CommitSHA == "" {
		return nil
	}
	projectID, pErr := projectIdentifier(e)
	if pErr != nil {
		h.log.Warn("gitlab deployment skipped: project cannot be identified",
			zap.String("run", e.RunName), zap.Error(pErr))
		return nil //nolint:nilerr // intentional: drop event if project cannot be identified
	}

	status, ok := deploymentStatus(e.State)
	if !ok {
		return nil // intermediate states GitLab's deployments API doesn't accept
	}

	environment := e.Context
	if environment == "" {
		environment = defaultEnvironment
	}

	ref := e.CommitSHA
	tag := false
	opts := &gl.CreateProjectDeploymentOptions{
		Environment: &environment,
		Ref:         &ref,
		SHA:         &e.CommitSHA,
		Tag:         &tag,
		Status:      &status,
	}
	_, _, err := h.client.gl.Deployments.CreateProjectDeployment(projectID, opts, gl.WithContext(ctx))
	return err
}

// deploymentStatus maps run states onto GitLab deployment statuses.
func deploymentStatus(s domain.State) (gl.DeploymentStatusValue, bool) {
	switch s {
	case domain.StateRunning:
		return gl.DeploymentStatusRunning, true
	case domain.StateSuccess:
		return gl.DeploymentStatusSuccess, true
	case domain.StateFailure, domain.StateError:
		return gl.DeploymentStatusFailed, true
	case domain.StateCanceled:
		return gl.DeploymentStatusCanceled, true
	default:
		return "", false
	}
}
