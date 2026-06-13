// Package sentry creates Sentry releases and deploy markers from pipeline
// events, associating errors with the deployed version (the CommitSHA).
package sentry

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const defaultBaseURL = "https://sentry.io"

// Notifier creates Sentry releases and deploys.
type Notifier struct {
	name     string
	baseURL  string
	token    string
	org      string
	projects []string
	http     *http.Client
	log      *zap.Logger
}

// Config configures the Sentry notifier.
type Config struct {
	Name     string
	BaseURL  string // empty = sentry.io
	Token    string
	Org      string
	Projects []string
	Log      *zap.Logger
}

// validateURL checks that a URL has an http or https scheme.
func validateURL(urlStr string) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", u.Scheme)
	}
}

// New creates a Sentry release notifier.
func New(cfg Config) *Notifier {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	if err := validateURL(base); err != nil {
		if cfg.Log != nil {
			cfg.Log.Error("invalid Sentry base URL, using default",
				zap.String("url", base),
				zap.Error(err),
			)
		}
		base = defaultBaseURL
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Notifier{
		name:     cfg.Name,
		baseURL:  strings.TrimRight(base, "/"),
		token:    cfg.Token,
		org:      cfg.Org,
		projects: cfg.Projects,
		http:     notifier.DefaultHTTPClient(),
		log:      log,
	}
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return n.name }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle creates (or reuses) the release for the commit and marks a deploy.
// Only successful runs produce releases; gate further with `when`.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	if e.State != domain.StateSuccess {
		return nil
	}
	if e.CommitSHA == "" {
		return nil
	}

	version := e.CommitSHA
	environment := e.Context
	if environment == "" {
		environment = "production"
	}

	// Creating an existing release is a no-op upsert in Sentry.
	releaseURL := fmt.Sprintf("%s/api/0/organizations/%s/releases/", n.baseURL, n.org)
	if err := n.post(ctx, releaseURL, map[string]any{
		"version":  version,
		"projects": n.projects,
		"refs": []map[string]string{
			{"repository": e.Repo.Owner + "/" + e.Repo.Name, "commit": e.CommitSHA},
		},
	}); err != nil {
		return fmt.Errorf("create release: %w", err)
	}

	deployURL := fmt.Sprintf("%s/api/0/organizations/%s/releases/%s/deploys/", n.baseURL, n.org, version)
	if err := n.post(ctx, deployURL, map[string]any{
		"environment": environment,
		"name":        e.RunName,
		"url":         e.TargetURL,
	}); err != nil {
		return fmt.Errorf("create deploy: %w", err)
	}
	return nil
}

func (n *Notifier) post(ctx context.Context, url string, payload any) error {
	req, err := httpx.NewJSONRequest(ctx, http.MethodPost, url, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+n.token)
	req.Header.Set("User-Agent", notifier.UserAgent)

	resp, err := httpx.DoWithRetryPolicy(n.http, req, httpx.DefaultRetryPolicy())
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("sentry API returned %d", resp.StatusCode)
	}
	return nil
}
