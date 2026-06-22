package github

import (
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// DiscussionCommentHandler posts comments to GitHub Discussions.
type DiscussionCommentHandler struct {
	client   HTTPDoer
	template *template.Template
}

// DiscussionCommentConfig configures the discussion comment handler.
type DiscussionCommentConfig struct {
	Client   HTTPDoer
	Template string
}

// NewDiscussionCommentHandler creates a new GitHub discussion comment handler.
func NewDiscussionCommentHandler(cfg DiscussionCommentConfig, _ *zap.Logger) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("discussion_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	return &DiscussionCommentHandler{
		client:   cfg.Client,
		template: tmpl,
	}, nil
}

// Name returns the handler name.
func (h *DiscussionCommentHandler) Name() string { return providerGitHub }

// Type returns the action type.
func (h *DiscussionCommentHandler) Type() notifier.ActionType {
	return notifier.ActionDiscussionComment
}

// Handle posts a comment to a GitHub Discussion. Returns nil (skip) if provider doesn't match,
// discussion number unavailable, or Discussions are disabled on the repository.
func (h *DiscussionCommentHandler) Handle(ctx context.Context, e domain.Event) error {
	// Provider-match guard
	if e.Provider != providerGitHub {
		return nil
	}

	// Skip if no discussion number available
	if e.DiscussionNumber == nil {
		return nil
	}

	// Validate required fields
	if e.Repo.Owner == "" || e.Repo.Name == "" {
		return nil
	}

	nodeID, err := h.resolveDiscussionNodeID(ctx, e.Repo.Owner, e.Repo.Name, *e.DiscussionNumber)
	if err != nil {
		return fmt.Errorf("resolve discussion node ID: %w", err)
	}

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate(providerGitHub, "comment_body", body); err != nil {
		return err
	}

	return h.addDiscussionComment(ctx, nodeID, body)
}

// resolveDiscussionNodeID queries GraphQL to get the global node ID for a discussion number.
func (h *DiscussionCommentHandler) resolveDiscussionNodeID(ctx context.Context, owner, repo string, number int) (string, error) {
	query := `
		query($owner: String!, $repo: String!, $number: Int!) {
			repository(owner: $owner, name: $repo) {
				discussion(number: $number) {
					id
				}
			}
		}
	`

	variables := map[string]any{
		"owner":  owner,
		"repo":   repo,
		"number": number,
	}

	data, err := h.client.DoGraphQL(ctx, query, variables)
	if err != nil {
		return "", err
	}

	var result struct {
		Repository struct {
			Discussion *struct {
				ID string `json:"id"`
			} `json:"discussion"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("unmarshal node ID response: %w", err)
	}

	if result.Repository.Discussion == nil {
		return "", fmt.Errorf("discussion #%d not found or Discussions disabled", number)
	}

	return result.Repository.Discussion.ID, nil
}

// addDiscussionComment posts a comment to a discussion via GraphQL mutation.
func (h *DiscussionCommentHandler) addDiscussionComment(ctx context.Context, discussionID, body string) error {
	mutation := `
		mutation($discussionId: ID!, $body: String!) {
			addDiscussionComment(input: {discussionId: $discussionId, body: $body}) {
				comment {
					id
				}
			}
		}
	`

	variables := map[string]any{
		"discussionId": discussionID,
		"body":         body, //nolint:goconst // GraphQL field
	}

	_, err := h.client.DoGraphQL(ctx, mutation, variables)
	return err
}
