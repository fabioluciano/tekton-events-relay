package azuredevops

import (
	"context"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	testToken   = "token"
	testBaseURL = "https://dev.azure.example.com"
)

func newTestCommentHandler(t *testing.T) notifier.ActionHandler {
	t.Helper()
	h, err := NewCommentHandler(CommentConfig{
		Token:    testToken,
		BaseURL:  testBaseURL,
		Genre:    "tekton",
		Template: "Run {{.RunName}}: {{.State}}",
		Log:      zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewCommentHandler: %v", err)
	}
	return h
}

func TestCommentHandler_NameAndType(t *testing.T) {
	h := newTestCommentHandler(t)
	if h.Name() != "azure-devops" {
		t.Errorf("Name = %q, want azure-devops", h.Name())
	}
	if h.Type() != notifier.ActionPRComment {
		t.Errorf("Type = %q, want pr_comment", h.Type())
	}
}

func TestCommentHandler_SkipsWrongProviderAndMissingFields(t *testing.T) {
	h := newTestCommentHandler(t)
	pr := 7

	e := azureEvent()
	e.PRNumber = &pr
	e.Provider = "github"
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle should skip wrong provider, got: %v", err)
	}

	e = azureEvent() // no PRNumber
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle should skip missing PR number, got: %v", err)
	}

	for _, mutate := range []func(*domain.Event){
		func(e *domain.Event) { e.Repo.Org = "" },
		func(e *domain.Event) { e.Repo.Project = "" },
		func(e *domain.Event) { e.Repo.Name = "" },
	} {
		e := azureEvent()
		e.PRNumber = &pr
		mutate(&e)
		if err := h.Handle(context.Background(), e); err != nil {
			t.Fatalf("Handle should skip missing fields, got: %v", err)
		}
	}
}

func TestCommentHandler_InvalidTemplateRejected(t *testing.T) {
	_, err := NewCommentHandler(CommentConfig{
		Token:    testToken,
		BaseURL:  testBaseURL,
		Template: "{{.Broken",
		Log:      zap.NewNop(),
	})
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}

func TestLabelHandler_NameAndType(t *testing.T) {
	h := NewLabelHandler(LabelConfig{
		Token: testToken, BaseURL: testBaseURL,
		Labels: scm.LabelSet{Add: []scm.Label{{Name: "ok"}}, Remove: []scm.Label{{Name: "bad"}}}, Log: zap.NewNop(),
	})
	if h.Name() != "azure-devops" {
		t.Errorf("Name = %q, want azure-devops", h.Name())
	}
	if h.Type() != notifier.ActionLabel {
		t.Errorf("Type = %q, want label", h.Type())
	}
}

func TestLabelHandler_SkipsWrongProviderAndMissingFields(t *testing.T) {
	h := NewLabelHandler(LabelConfig{
		Token: testToken, BaseURL: testBaseURL,
		Labels: scm.LabelSet{Add: []scm.Label{{Name: "ok"}}, Remove: []scm.Label{{Name: "bad"}}}, Log: zap.NewNop(),
	})
	pr := 7

	e := azureEvent()
	e.PRNumber = &pr
	e.Provider = "gitea"
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle should skip wrong provider, got: %v", err)
	}

	e = azureEvent() // no PRNumber
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle should skip missing PR number, got: %v", err)
	}

	// No label effect declared: must skip silently without any API call.
	empty := NewLabelHandler(LabelConfig{
		Token: testToken, BaseURL: testBaseURL, Log: zap.NewNop(),
	})
	e = azureEvent()
	e.PRNumber = &pr
	if err := empty.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle should skip empty label set, got: %v", err)
	}
}

// fakeGitClient is an in-memory prCommentClient for exercising the
// upsert/create paths without HTTP. It records calls and serves a seeded set
// of threads back to the handler.
type fakeGitClient struct {
	threads      []git.GitPullRequestCommentThread
	createCalls  int
	updateCalls  int
	getThreadErr error
	lastUpdated  string
	lastCreated  string
}

func (f *fakeGitClient) GetThreads(_ context.Context, _ git.GetThreadsArgs) (*[]git.GitPullRequestCommentThread, error) {
	if f.getThreadErr != nil {
		return nil, f.getThreadErr
	}
	threads := f.threads
	return &threads, nil
}

func (f *fakeGitClient) CreateThread(_ context.Context, args git.CreateThreadArgs) (*git.GitPullRequestCommentThread, error) {
	f.createCalls++
	if args.CommentThread != nil && args.CommentThread.Comments != nil {
		for _, c := range *args.CommentThread.Comments {
			if c.Content != nil {
				f.lastCreated = *c.Content
			}
		}
	}
	return &git.GitPullRequestCommentThread{}, nil
}

func (f *fakeGitClient) UpdateComment(_ context.Context, args git.UpdateCommentArgs) (*git.Comment, error) {
	f.updateCalls++
	if args.Comment != nil && args.Comment.Content != nil {
		f.lastUpdated = *args.Comment.Content
	}
	return &git.Comment{}, nil
}

func newUpsertHandler(t *testing.T, fake *fakeGitClient) *CommentHandler {
	t.Helper()
	ah, err := NewCommentHandler(CommentConfig{
		Token:    testToken,
		BaseURL:  testBaseURL,
		Genre:    "tekton",
		Template: "Run {{.RunName}}: {{.State}}",
		Mode:     scm.ModeUpsert,
		Log:      zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewCommentHandler: %v", err)
	}
	h := ah.(*CommentHandler)
	h.newGitClient = func(context.Context) (prCommentClient, error) { return fake, nil }
	return h
}

func TestCommentHandler_UpsertCreatesThenEditsSameComment(t *testing.T) {
	pr := 7
	fake := &fakeGitClient{}
	h := newUpsertHandler(t, fake)

	e := azureEvent()
	e.PRNumber = &pr
	e.RunID = "run-uid-1"

	// First call: no marked comment exists yet, so a thread is created.
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	if fake.createCalls != 1 || fake.updateCalls != 0 {
		t.Fatalf("after first call: create=%d update=%d, want create=1 update=0", fake.createCalls, fake.updateCalls)
	}

	marker := scm.Marker(e.RunID, "pr_comment")
	if !scm.HasMarker(fake.lastCreated, marker) {
		t.Fatalf("created body missing marker: %q", fake.lastCreated)
	}

	// Seed the created comment so the second call finds it via the marker.
	tid, cid := 11, 22
	fake.threads = []git.GitPullRequestCommentThread{
		{
			Id: &tid,
			Comments: &[]git.Comment{
				{Id: &cid, Content: &fake.lastCreated},
			},
		},
	}

	// Second call: marked comment found, edited via UpdateComment.
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	if fake.createCalls != 1 || fake.updateCalls != 1 {
		t.Fatalf("after second call: create=%d update=%d, want create=1 update=1", fake.createCalls, fake.updateCalls)
	}
	if !scm.HasMarker(fake.lastUpdated, marker) {
		t.Fatalf("updated body missing marker: %q", fake.lastUpdated)
	}
}

func TestCommentHandler_CreateModePostsThread(t *testing.T) {
	pr := 7
	fake := &fakeGitClient{}
	ah := newTestCommentHandler(t) // default mode = create
	h := ah.(*CommentHandler)
	h.newGitClient = func(context.Context) (prCommentClient, error) { return fake, nil }

	e := azureEvent()
	e.PRNumber = &pr

	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if fake.createCalls != 1 || fake.updateCalls != 0 {
		t.Fatalf("create mode: create=%d update=%d, want create=1 update=0", fake.createCalls, fake.updateCalls)
	}
	if scm.HasMarker(fake.lastCreated, scm.Marker(e.RunID, "pr_comment")) {
		t.Fatalf("create mode must not embed upsert marker: %q", fake.lastCreated)
	}
}

func TestCommentHandler_UpsertSkipsMissingPRWithoutAPICall(t *testing.T) {
	fake := &fakeGitClient{}
	h := newUpsertHandler(t, fake)

	e := azureEvent() // no PRNumber
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle should skip missing PR number, got: %v", err)
	}
	if fake.createCalls != 0 || fake.updateCalls != 0 {
		t.Fatalf("missing PR must make no API call: create=%d update=%d", fake.createCalls, fake.updateCalls)
	}
}

func TestCommentHandler_UpsertListFailureFallsBackToCreate(t *testing.T) {
	pr := 7
	fake := &fakeGitClient{getThreadErr: context.DeadlineExceeded}
	h := newUpsertHandler(t, fake)

	e := azureEvent()
	e.PRNumber = &pr
	e.RunID = "run-uid-2"

	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if fake.createCalls != 1 || fake.updateCalls != 0 {
		t.Fatalf("lookup failure must fall back to create: create=%d update=%d", fake.createCalls, fake.updateCalls)
	}
}
