package github

import (
	"context"
	"fmt"
	"text/template"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// postIssueComment posts (or upserts) a comment on a GitHub issue or PR —
// both share the issues comments API. action distinguishes the upsert
// marker so PR and issue comments for the same run stay independent.
func postIssueComment(ctx context.Context, client *Client, tmpl *template.Template, mode string, log *zap.Logger, e domain.Event, number int, action string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments",
		client.baseURL, e.Repo.Owner, e.Repo.Name, number)

	body, err := scm.RenderTemplate(tmpl, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if mode == scm.ModeUpsert {
		marker := scm.Marker(e.RunID, action)
		body = scm.WithMarker(marker, body)
		if err := scm.Validate(providerGitHub, "comment_body", body); err != nil {
			return err
		}
		if id, found := findMarkedComment(ctx, client, url, marker, log); found {
			editURL := fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d",
				client.baseURL, e.Repo.Owner, e.Repo.Name, id)
			return client.Do(ctx, "PATCH", editURL, map[string]string{"body": body}) //nolint:goconst // API field
		}
		return client.Do(ctx, "POST", url, map[string]string{"body": body})
	}

	if err := scm.Validate(providerGitHub, "comment_body", body); err != nil {
		return err
	}
	return client.Do(ctx, "POST", url, map[string]string{"body": body})
}

// issueComment is the subset of the GitHub comment payload used for upsert.
type issueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// findMarkedComment looks for an existing relay-managed comment carrying
// the marker. Lookup failures fall back to create so an API hiccup never
// blocks the notification.
func findMarkedComment(ctx context.Context, client *Client, listURL, marker string, log *zap.Logger) (int64, bool) {
	var comments []issueComment
	if err := client.DoWithResponse(ctx, "GET", listURL+"?per_page=100", nil, &comments); err != nil {
		log.Warn("upsert: listing comments failed, falling back to create", zap.Error(err))
		return 0, false
	}
	for _, c := range comments {
		if scm.HasMarker(c.Body, marker) {
			return c.ID, true
		}
	}
	return 0, false
}
