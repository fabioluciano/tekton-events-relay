// Package jira implements issue-tracking actions against Jira Cloud and
// Data Center: commenting on issues and transitioning their status when
// pipeline events arrive. The target issue key (PROJ-123) comes from the
// tekton.dev/tekton-events-relay.jira.issue-key annotation, extracted by
// the TriggerBinding from the branch name or PR title.
package jira

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"text/template"

	"go.uber.org/zap"

	jirav3 "github.com/ctreminiom/go-atlassian/v2/jira/v3"
	adfmodel "github.com/ctreminiom/go-atlassian/v2/pkg/infra/models"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const notifierName = "jira"

// ClientConfig holds connection and authentication settings.
type ClientConfig struct {
	BaseURL string // https://yourorg.atlassian.net (Cloud) or Data Center URL
	// APIVersion selects the Jira REST API: "3" sends Atlassian Document Format
	// comment bodies via go-atlassian; anything else (including "") uses the
	// plain-text REST v2 path.
	APIVersion string
	// Cloud: Email + Token (basic auth). Data Center: Token only (bearer PAT).
	Email string
	// Token resolves the credential fresh per request (file re-read or OAuth2
	// refresh), so rotated secrets and expiring tokens never go stale.
	Token              scm.TokenRefresher
	InsecureSkipVerify bool
	Debug              bool
}

// Client talks to Jira over REST. The v2 path uses BaseClient with plain-text
// bodies; when apiVersion is "3" the adf client posts ADF comment bodies.
type Client struct {
	*scm.BaseClient
	apiVersion string
	adf        *jirav3.Client
}

// NewClient builds a Jira client. With Email set it uses Cloud basic auth
// (email + token); otherwise the token is sent as a bearer (Data Center PAT or
// OAuth2 access token). The token is resolved per request via authTransport.
// When cfg.APIVersion is "3" a go-atlassian v3 client is wired through the same
// transport so ADF comment bodies can be posted.
func NewClient(cfg ClientConfig, log *zap.Logger) *Client {
	bc := scm.NewBaseClient(strings.TrimSuffix(cfg.BaseURL, "/"),
		cfg.InsecureSkipVerify, cfg.Debug, log, notifierName, nil)
	bc.HTTP.Transport = &authTransport{
		base:      bc.HTTP.Transport,
		email:     cfg.Email,
		refresher: cfg.Token,
	}
	c := &Client{BaseClient: bc, apiVersion: cfg.APIVersion}
	if cfg.APIVersion == "3" {
		adf, err := jirav3.New(bc.HTTP, bc.BaseURL)
		if err != nil {
			log.Error("jira: build v3 client", zap.Error(err))
		} else {
			c.adf = adf
		}
	}
	return c
}

// authTransport injects a freshly resolved Jira credential on every request.
// Cloud uses basic auth (email + token); Data Center / OAuth2 uses Bearer.
type authTransport struct {
	base      http.RoundTripper
	email     string
	refresher scm.TokenRefresher
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.refresher.Token(req.Context())
	if err != nil {
		return nil, fmt.Errorf("jira: resolve token: %w", err)
	}
	req = req.Clone(req.Context())
	if t.email != "" {
		req.SetBasicAuth(t.email, tok)
	} else {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return t.base.RoundTrip(req)
}

// CommentHandler posts a comment on the linked Jira issue.
type CommentHandler struct {
	client *Client
	tmpl   *template.Template
	log    *zap.Logger
}

// NewCommentHandler builds a comment handler; template must come from ConfigMap.
func NewCommentHandler(client *Client, tmplSrc string, log *zap.Logger) (*CommentHandler, error) {
	if tmplSrc == "" {
		return nil, fmt.Errorf("jira: comment template is required (must be provided via ConfigMap)")
	}
	tmpl, err := scm.CompileTemplate("jira_comment", tmplSrc, nil)
	if err != nil {
		return nil, fmt.Errorf("jira comment template: %w", err)
	}
	return &CommentHandler{client: client, tmpl: tmpl, log: log}, nil
}

// Name returns the provider identifier.
func (h *CommentHandler) Name() string { return notifierName }

// Provider returns the provider type identifier.
func (h *CommentHandler) Provider() string { return notifierName }

// Type returns the action type.
func (h *CommentHandler) Type() notifier.ActionType { return notifier.ActionJiraComment }

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *CommentHandler) Close() error { return nil }

// Handle posts the rendered comment; events without an issue key are skipped.
func (h *CommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.JiraIssueKey == "" {
		return nil
	}
	var body strings.Builder
	if err := h.tmpl.Execute(&body, e); err != nil {
		return fmt.Errorf("jira: render comment: %w", err)
	}
	if h.client.apiVersion == "3" {
		return h.handleV3(ctx, e, body.String())
	}
	url := fmt.Sprintf("%s/rest/api/2/issue/%s/comment", h.client.BaseURL, e.JiraIssueKey)
	if err := h.client.Do(ctx, http.MethodPost, url, map[string]string{"body": body.String()}); err != nil {
		return fmt.Errorf("jira: comment on %s: %w", e.JiraIssueKey, err)
	}
	h.log.Debug("jira comment posted", zap.String("issue", e.JiraIssueKey), zap.String("run", e.RunName))
	return nil
}

// handleV3 posts the comment through go-atlassian's REST v3 endpoint, wrapping
// the rendered text in a single-paragraph Atlassian Document Format body.
func (h *CommentHandler) handleV3(ctx context.Context, e domain.Event, text string) error {
	if h.client.adf == nil {
		return fmt.Errorf("jira: v3 client unavailable for %s", e.JiraIssueKey)
	}
	if _, _, err := h.client.adf.Issue.Comment.Add(ctx, e.JiraIssueKey, adfCommentBody(text), nil); err != nil {
		return fmt.Errorf("jira: comment on %s: %w", e.JiraIssueKey, err)
	}
	h.log.Debug("jira comment posted (v3/adf)", zap.String("issue", e.JiraIssueKey), zap.String("run", e.RunName))
	return nil
}

// adfCommentBody wraps plain text in a minimal ADF document: a "doc" root with
// one paragraph holding a single text node.
func adfCommentBody(text string) *adfmodel.CommentPayloadScheme {
	return &adfmodel.CommentPayloadScheme{
		Body: &adfmodel.CommentNodeScheme{
			Version: 1,
			Type:    "doc",
			Content: []*adfmodel.CommentNodeScheme{
				{
					Type: "paragraph",
					Content: []*adfmodel.CommentNodeScheme{
						{Type: "text", Text: text},
					},
				},
			},
		},
	}
}

// TransitionHandler moves the linked issue to the configured status.
type TransitionHandler struct {
	client     *Client
	transition string // target transition name (case-insensitive) or numeric id
	log        *zap.Logger
}

// NewTransitionHandler builds a transition handler.
func NewTransitionHandler(client *Client, transition string, log *zap.Logger) (*TransitionHandler, error) {
	if transition == "" {
		return nil, fmt.Errorf("jira: transition name is required")
	}
	return &TransitionHandler{client: client, transition: transition, log: log}, nil
}

// Name returns the provider identifier.
func (h *TransitionHandler) Name() string { return notifierName }

// Provider returns the provider type identifier.
func (h *TransitionHandler) Provider() string { return notifierName }

// Type returns the action type.
func (h *TransitionHandler) Type() notifier.ActionType { return notifier.ActionJiraTransition }

// Close is a no-op; this handler holds no resources requiring cleanup.
func (h *TransitionHandler) Close() error { return nil }

type transitionList struct {
	Transitions []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"transitions"`
}

// Handle resolves the transition by name and applies it; events without an
// issue key are skipped, and an unavailable transition is a no-op (the issue
// is simply not in a state that allows it).
func (h *TransitionHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.JiraIssueKey == "" {
		return nil
	}

	listURL := fmt.Sprintf("%s/rest/api/2/issue/%s/transitions", h.client.BaseURL, e.JiraIssueKey)
	var list transitionList
	if err := h.client.DoWithResponse(ctx, http.MethodGet, listURL, nil, &list); err != nil {
		return fmt.Errorf("jira: list transitions for %s: %w", e.JiraIssueKey, err)
	}

	id := ""
	for _, t := range list.Transitions {
		if strings.EqualFold(t.Name, h.transition) || t.ID == h.transition {
			id = t.ID
			break
		}
	}
	if id == "" {
		h.log.Debug("jira transition not available, skipping",
			zap.String("issue", e.JiraIssueKey), zap.String("transition", h.transition))
		return nil
	}

	payload := map[string]map[string]string{"transition": {"id": id}}
	if err := h.client.Do(ctx, http.MethodPost, listURL, payload); err != nil {
		return fmt.Errorf("jira: transition %s to %q: %w", e.JiraIssueKey, h.transition, err)
	}
	h.log.Debug("jira issue transitioned",
		zap.String("issue", e.JiraIssueKey), zap.String("transition", h.transition))
	return nil
}
