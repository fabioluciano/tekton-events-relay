package gitlab

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

func newIssueHandler(t *testing.T, baseURL, mode string) notifier.ActionHandler {
	t.Helper()
	client, err := NewClient("token", baseURL, false, false, zap.NewNop())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	h, err := NewIssueCommentHandler(IssueCommentConfig{
		Client: client, Name: "gitlab-main", //nolint:goconst // test string
		Template: "Run {{.RunName}}: {{.State}}", Mode: mode, Log: zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewIssueCommentHandler: %v", err)
	}
	return h
}

func issueEvent(state domain.State) domain.Event {
	issue := 9
	return domain.Event{
		Provider: "gitlab-main",
		Repo:     domain.Repo{ID: "42"},
		RunName:  "run-1", RunID: "uid-123",
		IssueNumber: &issue, State: state,
	}
}

func TestIssueCommentHandler_Type(t *testing.T) {
	h := newIssueHandler(t, "", "")
	if got := h.Type(); got != notifier.ActionIssueComment {
		t.Errorf("Type() = %q, want %q", got, notifier.ActionIssueComment)
	}
	if got := h.Name(); got != "gitlab-main" {
		t.Errorf("Name() = %q, want %q", got, "gitlab-main")
	}
}

func TestIssueCommentHandler_CreatesNote(t *testing.T) {
	mock := &notesMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newIssueHandler(t, srv.URL, "")
	if err := h.Handle(context.Background(), issueEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if mock.creates != 1 {
		t.Errorf("creates = %d, want 1", mock.creates)
	}
}

func TestIssueCommentHandler_UpsertEditsExistingNote(t *testing.T) {
	mock := &notesMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newIssueHandler(t, srv.URL, "upsert")
	if err := h.Handle(context.Background(), issueEvent(domain.StateRunning)); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	if err := h.Handle(context.Background(), issueEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	if mock.creates != 1 || mock.updates != 1 {
		t.Errorf("creates=%d updates=%d, want 1/1", mock.creates, mock.updates)
	}
	if body := mock.notes[0]["body"].(string); !strings.HasPrefix(body, "<!-- tekton-events-relay:uid-123:issue_comment -->") {
		t.Errorf("missing marker: %q", body)
	}
}

func TestIssueCommentHandler_Skips(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.Event)
	}{
		{"wrong provider", func(e *domain.Event) { e.Provider = "github" }}, //nolint:goconst // test string
		{"nil issue number", func(e *domain.Event) { e.IssueNumber = nil }},
		{"no project identifier", func(e *domain.Event) { e.Repo = domain.Repo{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &notesMock{}
			srv := httptest.NewServer(mock.handler())
			defer srv.Close()

			h := newIssueHandler(t, srv.URL, "")
			e := issueEvent(domain.StateSuccess)
			tt.mutate(&e)
			_ = h.Handle(context.Background(), e)

			if mock.creates != 0 {
				t.Errorf("creates = %d, want 0", mock.creates)
			}
		})
	}
}

func TestIssueCommentHandler_InvalidModeRejected(t *testing.T) {
	client, err := NewClient("t", "", false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewIssueCommentHandler(IssueCommentConfig{Client: client, Name: "g", Mode: "replace", Log: zap.NewNop()})
	if err == nil {
		t.Fatal("expected error")
	}
}
