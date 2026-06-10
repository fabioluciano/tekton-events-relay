package azuredevops

import (
	"context"
	"fmt"
	"text/template"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// CommentHandler posts comments to Azure DevOps pull requests.
type CommentHandler struct {
	client   *Client
	template *template.Template
}

// CommentConfig configures the comment handler.
type CommentConfig struct {
	Token              string
	BaseURL            string
	Genre              string
	Template           string
	InsecureSkipVerify bool
	Log                *zap.Logger
}

// NewCommentHandler creates a new Azure DevOps PR comment handler.
func NewCommentHandler(cfg CommentConfig) (notifier.ActionHandler, error) {
	var tmpl *template.Template
	if cfg.Template != "" {
		var err error
		tmpl, err = scm.CompileTemplate("pr_comment", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
	}

	return &CommentHandler{
		client:   NewClient(cfg.Token, cfg.BaseURL, cfg.Genre, cfg.InsecureSkipVerify, false, cfg.Log),
		template: tmpl,
	}, nil
}

// Name returns the handler name.
func (h *CommentHandler) Name() string { return providerAzure }

// Type returns the action type.
func (h *CommentHandler) Type() notifier.ActionType { return notifier.ActionPRComment }

// Handle posts a comment to an Azure DevOps PR.
func (h *CommentHandler) Handle(ctx context.Context, e domain.Event) error {
	if e.Provider != providerAzure {
		return nil
	}

	if e.PRNumber == nil || e.Repo.Org == "" || e.Repo.Project == "" || e.Repo.Name == "" {
		return nil
	}

	body, err := scm.RenderTemplate(h.template, e)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := scm.Validate(providerAzure, "comment_body", body); err != nil {
		return err
	}

	gitClient, err := git.NewClient(ctx, h.client.conn)
	if err != nil {
		return err
	}

	commentType := git.CommentTypeValues.Text
	status := git.CommentThreadStatusValues.Active

	thread := git.GitPullRequestCommentThread{
		Comments: &[]git.Comment{
			{
				Content:     &body,
				CommentType: &commentType,
			},
		},
		Status: &status,
	}

	prID := *e.PRNumber
	_, err = gitClient.CreateThread(ctx, git.CreateThreadArgs{
		CommentThread: &thread,
		RepositoryId:  &e.Repo.Name,
		PullRequestId: &prID,
		Project:       &e.Repo.Project,
	})

	return err
}
