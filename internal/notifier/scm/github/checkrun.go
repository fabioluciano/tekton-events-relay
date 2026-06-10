// Package github provides GitHub SCM integrations for commit status, check runs, comments, and labels.
package github

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// CheckRunConfig holds GitHub Check Run handler configuration.
type CheckRunConfig struct {
	Token              string
	BaseURL            string
	Name               string // check run name displayed in GitHub UI
	Template           string // optional Go template for markdown summary
	InsecureSkipVerify bool
}

// CheckRunHandler reports pipeline status as GitHub Check Runs.
// Requires a GitHub App token with checks:write permission.
type CheckRunHandler struct {
	client *Client
	name   string
	tmpl   *template.Template
	log    *zap.Logger
}

// NewCheckRunHandler creates a Check Run handler.
func NewCheckRunHandler(cfg CheckRunConfig, log *zap.Logger) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("checkrun", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	checkName := cfg.Name
	if checkName == "" {
		checkName = "Tekton Pipeline"
	}

	return &CheckRunHandler{
		client: NewClient(cfg.Token, cfg.BaseURL, cfg.InsecureSkipVerify, log, false),
		name:   checkName,
		tmpl:   tmpl,
		log:    log,
	}, nil
}

// Name returns the provider identifier.
func (h *CheckRunHandler) Name() string { return providerGitHub }

// Type returns the action type.
func (h *CheckRunHandler) Type() notifier.ActionType { return notifier.ActionCheckRun }

// Handle creates or updates a GitHub Check Run for the event.
func (h *CheckRunHandler) Handle(ctx context.Context, e domain.Event) error {
	// Provider-match guard
	if e.Provider != providerGitHub {
		h.log.Debug("check run skipped: provider mismatch",
			zap.String("provider", e.Provider),
			zap.String("namespace", e.Namespace),
			zap.String("taskrun", e.RunName))
		return nil
	}

	// Validate required fields
	if e.CommitSHA == "" {
		h.log.Info("check run NOT sent: missing scm.commit-sha annotation",
			zap.String("namespace", e.Namespace),
			zap.String("taskrun", e.RunName))
		return nil
	}

	if e.Repo.Owner == "" || e.Repo.Name == "" {
		h.log.Warn("check run NOT sent: missing scm.repo-owner or scm.repo-name annotation",
			zap.String("namespace", e.Namespace),
			zap.String("taskrun", e.RunName))
		return nil
	}

	// Map state to Check Run status/conclusion
	status, conclusion := h.mapState(e.State)

	// Build payload
	payload := map[string]any{
		"name":        h.name,
		"head_sha":    e.CommitSHA,
		"status":      status,
		"external_id": e.RunID,
	}

	if conclusion != "" {
		payload["conclusion"] = conclusion
		payload["completed_at"] = time.Now().UTC().Format(time.RFC3339)
	}

	if status == statusInProgress && !e.StartedAt.IsZero() {
		payload["started_at"] = e.StartedAt.UTC().Format(time.RFC3339)
	}

	// Generate summary
	summary := h.generateSummary(&e)
	if summary != "" {
		payload["output"] = map[string]any{
			"title":   fmt.Sprintf("Pipeline: %s", e.RunName),
			"summary": summary,
		}
	}

	url := fmt.Sprintf("%s/repos/%s/%s/check-runs",
		h.client.baseURL, e.Repo.Owner, e.Repo.Name)

	return h.client.Do(ctx, "POST", url, payload)
}

// mapState converts domain.State to Check Run status and conclusion.
func (h *CheckRunHandler) mapState(state domain.State) (status, conclusion string) {
	switch state {
	case domain.StatePending:
		return statusQueued, ""
	case domain.StateRunning:
		return statusInProgress, ""
	case domain.StateSuccess:
		return statusCompleted, stateSuccess
	case domain.StateFailure:
		return statusCompleted, stateFailure
	case domain.StateError:
		return statusCompleted, stateFailure
	case domain.StateCanceled:
		return statusCompleted, stateCancelled
	default:
		return statusQueued, ""
	}
}

// generateSummary renders markdown summary from template or default.
func (h *CheckRunHandler) generateSummary(e *domain.Event) string {
	if h.tmpl == nil {
		return fmt.Sprintf("**Pipeline:** %s\n**Status:** %s", e.RunName, e.State)
	}

	var buf bytes.Buffer
	if err := h.tmpl.Execute(&buf, e); err != nil {
		h.log.Warn("check run template execution failed", zap.Error(err))
		return ""
	}
	return buf.String()
}
