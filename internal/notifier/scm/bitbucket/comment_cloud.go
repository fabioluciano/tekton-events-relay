package bitbucket

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// CloudCommentHandler posts comments to Bitbucket Cloud pull requests.
type CloudCommentHandler struct {
	client   *CloudClient
	template *template.Template
	mode     string
	log      *zap.Logger
}

// CloudCommentConfig configures the Cloud comment handler.
type CloudCommentConfig struct {
	Username           string
	AppPassword        string
	BaseURL            string
	Template           string
	Mode               string // scm.ModeCreate (default) or scm.ModeUpsert
	InsecureSkipVerify bool
	Log                *zap.Logger
	// Client, when non-nil, is used instead of building one from Username/AppPassword.
	// Set this for OAuth2 auth where the client resolves tokens per-request.
	Client *CloudClient
}

// NewCloudCommentHandler creates a new Bitbucket Cloud PR comment handler.
func NewCloudCommentHandler(cfg CloudCommentConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("pr_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	mode, err := scm.NormalizeMode(cfg.Mode)
	if err != nil {
		return nil, err
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}

	client := cfg.Client
	if client == nil {
		client = NewCloudClient(cfg.Username, cfg.AppPassword, cfg.BaseURL, cfg.InsecureSkipVerify, false, cfg.Log)
	}

	return &CloudCommentHandler{
		client:   client,
		template: tmpl,
		mode:     mode,
		log:      log,
	}, nil
}

// Name returns the handler name.
func (h *CloudCommentHandler) Name() string { return providerCloud }

// Type returns the action type.
func (h *CloudCommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a comment to a Bitbucket Cloud PR.
func (h *CloudCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerCloud {
		return nil
	}

	if e.PRNumber == nil {
		return nil
	}

	ws := e.Repo.Workspace
	if ws == "" {
		ws = e.Repo.Owner
	}
	if ws == "" || e.Repo.Name == "" {
		return nil
	}

	url := fmt.Sprintf("%s/2.0/repositories/%s/%s/pullrequests/%d/comments",
		strings.TrimRight(h.client.BaseURL, "/"), ws, e.Repo.Name, *e.PRNumber)

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if h.mode == scm.ModeUpsert {
		marker := scm.Marker(e.RunID, "pr_comment")
		body = scm.WithMarker(marker, body)
		if err := scm.Validate(providerCloud, "comment_body", body); err != nil {
			return err
		}
		payload := map[string]any{"content": map[string]string{"raw": body}}
		if id, found := h.findMarkedComment(ctx, url, marker); found {
			return h.client.Do(ctx, "PUT", fmt.Sprintf("%s/%d", url, id), payload)
		}
		return h.client.Do(ctx, "POST", url, payload)
	}

	if err := scm.Validate(providerCloud, "comment_body", body); err != nil {
		return err
	}

	payload := map[string]any{
		"content": map[string]string{
			"raw": body,
		},
	}

	return h.client.Do(ctx, "POST", url, payload)
}

// cloudCommentList is the subset of Bitbucket Cloud's paginated comment
// listing used for upsert.
type cloudCommentList struct {
	Values []struct {
		ID      int64 `json:"id"`
		Content struct {
			Raw string `json:"raw"`
		} `json:"content"`
	} `json:"values"`
}

// findMarkedComment looks for an existing relay-managed comment carrying
// the marker. Lookup failures fall back to create.
func (h *CloudCommentHandler) findMarkedComment(ctx context.Context, listURL, marker string) (int64, bool) {
	var list cloudCommentList
	if err := h.client.DoWithResponse(ctx, "GET", listURL+"?pagelen=100", nil, &list); err != nil {
		h.log.Warn("upsert: listing comments failed, falling back to create", zap.Error(err))
		return 0, false
	}
	for _, c := range list.Values {
		if scm.HasMarker(c.Content.Raw, marker) {
			return c.ID, true
		}
	}
	return 0, false
}
