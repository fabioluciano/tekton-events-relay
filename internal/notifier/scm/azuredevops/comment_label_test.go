package azuredevops

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

func newTestCommentHandler(t *testing.T) notifier.ActionHandler {
	t.Helper()
	h, err := NewCommentHandler(CommentConfig{
		Token:    "token",
		BaseURL:  "https://dev.azure.example.com",
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
		Token:    "token",
		BaseURL:  "https://dev.azure.example.com",
		Template: "{{.Broken",
		Log:      zap.NewNop(),
	})
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}

func TestLabelHandler_NameAndType(t *testing.T) {
	h := NewLabelHandler(LabelConfig{
		Token: "token", BaseURL: "https://dev.azure.example.com",
		SuccessLabel: "ok", FailureLabel: "bad", Log: zap.NewNop(),
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
		Token: "token", BaseURL: "https://dev.azure.example.com",
		SuccessLabel: "ok", FailureLabel: "bad", Log: zap.NewNop(),
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

	// Non-terminal state has no label configured: must skip silently.
	e = azureEvent()
	e.PRNumber = &pr
	e.State = domain.StateRunning
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle should skip state without label, got: %v", err)
	}
}
