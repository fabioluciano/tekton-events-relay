package gitea

import (
	"fmt"
	"text/template"

	giteaSDK "code.gitea.io/sdk/gitea"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// postComment posts (or upserts) a comment on a Gitea issue or PR — both
// share the issue comments API. action distinguishes the upsert marker.
func postComment(client *Client, tmpl *template.Template, mode string, log *zap.Logger, e domain.Event, index int64, action string) error {
	body, err := scm.RenderTemplate(tmpl, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if mode == scm.ModeUpsert {
		marker := scm.Marker(e.RunID, action)
		body = scm.WithMarker(marker, body)
		if err := scm.Validate(providerGitea, "comment_body", body); err != nil {
			return fmt.Errorf("validate comment body: %w", err)
		}
		return upsertComment(client, e.Repo.Owner, e.Repo.Name, index, marker, body, log)
	}

	if err := scm.Validate(providerGitea, "comment_body", body); err != nil {
		return fmt.Errorf("validate comment body: %w", err)
	}

	_, _, err = client.sdk.CreateIssueComment(e.Repo.Owner, e.Repo.Name, index,
		giteaSDK.CreateIssueCommentOption{Body: body})
	return err
}

// upsertComment edits the existing relay-managed comment carrying the
// marker, or creates one if absent. Lookup failures fall back to create.
func upsertComment(client *Client, owner, repo string, index int64, marker, body string, log *zap.Logger) error {
	comments, _, err := client.sdk.ListIssueComments(owner, repo, index, giteaSDK.ListIssueCommentOptions{})
	if err != nil {
		log.Warn("upsert: listing comments failed, falling back to create", zap.Error(err))
	} else {
		for _, c := range comments {
			if scm.HasMarker(c.Body, marker) {
				_, _, err := client.sdk.EditIssueComment(owner, repo, c.ID, giteaSDK.EditIssueCommentOption{Body: body})
				return err
			}
		}
	}
	_, _, err = client.sdk.CreateIssueComment(owner, repo, index, giteaSDK.CreateIssueCommentOption{Body: body})
	return err
}
