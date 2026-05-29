package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// PREnricher optionally enriches events with PR numbers via API lookup.
// Used as fallback when annotations are not present.
type PREnricher struct {
	BaseHandler
	client        *http.Client
	providers     map[string]PRLookupConfig // provider → config
	enabledByName map[string]bool
	log           *zap.Logger
}

// PRLookupConfig configures API-lookup enrichment per provider.
type PRLookupConfig struct {
	Enabled bool
	Token   string
	BaseURL string
}

// NewPREnricher creates a new PREnricher with per-provider configs.
func NewPREnricher(configs map[string]PRLookupConfig, log *zap.Logger) *PREnricher {
	enabled := make(map[string]bool)
	for name, cfg := range configs {
		enabled[name] = cfg.Enabled
	}

	return &PREnricher{
		client:        &http.Client{Timeout: 5 * time.Second},
		providers:     configs,
		enabledByName: enabled,
		log:           log,
	}
}

// Handle enriches the event with PR number via API lookup if not already present.
func (e *PREnricher) Handle(ctx context.Context, env *event.Envelope) error {
	evt := &env.Report

	// Skip if numbers already present
	if evt.PRNumber != nil || evt.IssueNumber != nil {
		return e.Next(ctx, env)
	}

	// Skip if provider lookup not enabled
	if !e.enabledByName[evt.Provider] {
		return e.Next(ctx, env)
	}

	// Lookup PR by commit SHA (GitHub example - other providers need similar logic)
	if evt.Provider == "github" {
		prNum, err := e.lookupGitHubPR(ctx, evt)
		if err != nil {
			e.log.Warn("PR lookup failed",
				zap.String("provider", "github"),
				zap.String("commit_sha", evt.CommitSHA),
				zap.Error(err))
		} else if prNum != nil {
			evt.PRNumber = prNum
			e.log.Debug("PR number enriched via API lookup",
				zap.String("provider", "github"),
				zap.Int("pr_number", *prNum))
		}
	}

	return e.Next(ctx, env)
}

// lookupGitHubPR queries GitHub API to find PR associated with commit SHA.
func (e *PREnricher) lookupGitHubPR(ctx context.Context, evt *domain.Event) (*int, error) {
	cfg := e.providers["github"]
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s/pulls",
		cfg.BaseURL, evt.Repo.Owner, evt.Repo.Name, evt.CommitSHA)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+cfg.Token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var prs []struct {
		Number int `json:"number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		return nil, err
	}

	if len(prs) == 0 {
		return nil, nil // No PR found
	}

	return &prs[0].Number, nil // Return first PR
}
