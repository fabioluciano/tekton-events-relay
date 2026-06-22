package github

import (
	"context"
	"fmt"
	"text/template"

	gh "github.com/google/go-github/v68/github"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// postIssueComment posts (or upserts) a comment on a GitHub issue or PR —
// both share the issues comments API. action distinguishes the upsert
// marker so PR and issue comments for the same run stay independent.
func postIssueComment(ctx context.Context, client HTTPDoer, tmpl *template.Template, mode string, log *zap.Logger, e domain.Event, number int, action string) error {
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
		if id, found := findMarkedComment(ctx, client, e.Repo.Owner, e.Repo.Name, number, marker, log); found {
			_, _, err = client.GH().Issues.EditComment(ctx, e.Repo.Owner, e.Repo.Name, id, &gh.IssueComment{Body: gh.Ptr(body)})
			return err
		}
		_, _, err = client.GH().Issues.CreateComment(ctx, e.Repo.Owner, e.Repo.Name, number, &gh.IssueComment{Body: gh.Ptr(body)})
		return err
	}

	if err := scm.Validate(providerGitHub, "comment_body", body); err != nil {
		return err
	}
	_, _, err = client.GH().Issues.CreateComment(ctx, e.Repo.Owner, e.Repo.Name, number, &gh.IssueComment{Body: gh.Ptr(body)})
	return err
}

// findMarkedComment looks for an existing relay-managed comment carrying
// the marker. Lookup failures fall back to create so an API hiccup never
// blocks the notification.
func findMarkedComment(ctx context.Context, client HTTPDoer, owner, repo string, number int, marker string, log *zap.Logger) (int64, bool) {
	opts := &gh.IssueListCommentsOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	comments, _, err := client.GH().Issues.ListComments(ctx, owner, repo, number, opts)
	if err != nil {
		log.Warn("upsert: listing comments failed, falling back to create", zap.Error(err))
		return 0, false
	}
	for _, c := range comments {
		if scm.HasMarker(c.GetBody(), marker) {
			return c.GetID(), true
		}
	}
	return 0, false
}
