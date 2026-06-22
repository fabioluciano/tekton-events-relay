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

// prCommentClient is the narrow subset of the Azure DevOps git SDK client the
// comment handler depends on. The official *git.Client satisfies it; tests
// inject a fake to exercise the upsert/create paths without HTTP.
type prCommentClient interface {
	GetThreads(ctx context.Context, args git.GetThreadsArgs) (*[]git.GitPullRequestCommentThread, error)
	CreateThread(ctx context.Context, args git.CreateThreadArgs) (*git.GitPullRequestCommentThread, error)
	UpdateComment(ctx context.Context, args git.UpdateCommentArgs) (*git.Comment, error)
}

// CommentHandler posts comments to Azure DevOps pull requests.
type CommentHandler struct {
	client   *Client
	template *template.Template
	mode     string
	log      *zap.Logger
	// newGitClient builds the git SDK client per request; overridable in tests.
	newGitClient func(ctx context.Context) (prCommentClient, error)
}

// CommentConfig configures the comment handler.
type CommentConfig struct {
	Token              string
	BaseURL            string
	Genre              string
	Template           string
	Mode               string // scm.ModeCreate (default) or scm.ModeUpsert
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

	mode, err := scm.NormalizeMode(cfg.Mode)
	if err != nil {
		return nil, err
	}

	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}

	client := NewClient(cfg.Token, cfg.BaseURL, cfg.Genre, cfg.InsecureSkipVerify, false, log)

	h := &CommentHandler{
		client:   client,
		template: tmpl,
		mode:     mode,
		log:      log,
	}
	h.newGitClient = func(ctx context.Context) (prCommentClient, error) {
		return git.NewClient(ctx, client.conn)
	}
	return h, nil
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

	gitClient, err := h.newGitClient(ctx)
	if err != nil {
		return err
	}

	if h.mode == scm.ModeUpsert {
		return h.upsert(ctx, gitClient, e, body)
	}

	if err := scm.Validate(providerAzure, "comment_body", body); err != nil {
		return err
	}
	return h.create(ctx, gitClient, e, body)
}

// upsert embeds an invisible marker in the comment body and edits the existing
// marked comment if present, otherwise creates a new thread. Lookup failures
// fall back to create so a transient read error never drops the comment.
func (h *CommentHandler) upsert(ctx context.Context, gitClient prCommentClient, e domain.Event, body string) error {
	marker := scm.Marker(e.RunID, "pr_comment")
	body = scm.WithMarker(marker, body)
	if err := scm.Validate(providerAzure, "comment_body", body); err != nil {
		return err
	}

	if threadID, commentID, found := h.findMarkedComment(ctx, gitClient, e, marker); found {
		prID := *e.PRNumber
		_, err := gitClient.UpdateComment(ctx, git.UpdateCommentArgs{
			Comment:       &git.Comment{Content: &body},
			RepositoryId:  &e.Repo.Name,
			PullRequestId: &prID,
			ThreadId:      &threadID,
			CommentId:     &commentID,
			Project:       &e.Repo.Project,
		})
		return err
	}

	return h.create(ctx, gitClient, e, body)
}

// create posts a new comment thread to the PR.
func (h *CommentHandler) create(ctx context.Context, gitClient prCommentClient, e domain.Event, body string) error {
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
	_, err := gitClient.CreateThread(ctx, git.CreateThreadArgs{
		CommentThread: &thread,
		RepositoryId:  &e.Repo.Name,
		PullRequestId: &prID,
		Project:       &e.Repo.Project,
	})
	return err
}

// findMarkedComment scans PR threads for a relay-managed comment carrying the
// marker, returning its thread and comment IDs. Lookup failures fall back to
// create.
func (h *CommentHandler) findMarkedComment(ctx context.Context, gitClient prCommentClient, e domain.Event, marker string) (threadID, commentID int, found bool) {
	prID := *e.PRNumber
	threads, err := gitClient.GetThreads(ctx, git.GetThreadsArgs{
		RepositoryId:  &e.Repo.Name,
		PullRequestId: &prID,
		Project:       &e.Repo.Project,
	})
	if err != nil {
		h.log.Warn("upsert: listing PR threads failed, falling back to create", zap.Error(err))
		return 0, 0, false
	}
	if threads == nil {
		return 0, 0, false
	}

	for _, thread := range *threads {
		if thread.Id == nil || thread.Comments == nil {
			continue
		}
		for _, c := range *thread.Comments {
			if c.Id == nil || c.Content == nil {
				continue
			}
			if scm.HasMarker(*c.Content, marker) {
				return *thread.Id, *c.Id, true
			}
		}
	}
	return 0, 0, false
}
