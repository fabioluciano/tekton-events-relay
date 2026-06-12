package azuredevops

import (
	"context"
	"testing"

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
