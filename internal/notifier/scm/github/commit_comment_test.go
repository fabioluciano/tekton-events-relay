package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func TestCommitCommentHandler_PostsOnCommit(t *testing.T) {
	var path string
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		path = r.URL.Path
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	h, err := NewCommitCommentHandler(CommitCommentConfig{
		Token: testHandlerToken, BaseURL: srv.URL, Template: "Run {{.RunName}}",
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	e := domain.Event{
		Provider:  providerGitHub,
		Repo:      domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		CommitSHA: "abc123",
		RunName:   "run-1",
		State:     domain.StateSuccess,
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if calls != 1 || !strings.Contains(path, "/commits/abc123/comments") {
		t.Errorf("calls=%d path=%q, want commit comments endpoint", calls, path)
	}

	// Skips: wrong provider and missing SHA must not call the API.
	e.Provider = "gitea"
	_ = h.Handle(context.Background(), e)
	e.Provider = providerGitHub
	e.CommitSHA = ""
	_ = h.Handle(context.Background(), e)
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (skips)", calls)
	}
}
