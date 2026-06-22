package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/middleware"
)

const (
	testDiscussToken     = "test-token"
	testDiscussAPIURL    = "https://api.github.com"
	testDiscussOwner     = "test-owner"
	testDiscussRepo      = "test-repo"
	testDiscussNodeID    = "D_kwDOABCDEF"
	testDiscussCommentID = "DC_kwDOABCDEF"
)

func TestDiscussionCommentHandler_HappyPath(t *testing.T) {
	discussionNum := 42
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		query, ok := req["query"].(string)
		if !ok {
			t.Fatal("expected query in request")
		}

		// First call: node ID resolution
		if strContains(query, "query($owner:") && strContains(query, "discussion(number:") {
			resp := map[string]any{
				"data": map[string]any{ //nolint:goconst
					"repository": map[string]any{ //nolint:goconst
						"discussion": map[string]any{ //nolint:goconst
							"id": testDiscussNodeID,
						},
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Second call: add comment mutation
		if strContains(query, "mutation($discussionId:") && strContains(query, "addDiscussionComment") {
			resp := map[string]any{
				"data": map[string]any{
					"addDiscussionComment": map[string]any{
						"comment": map[string]any{
							"id": "DC_kwDOABCDEF",
						},
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		t.Errorf("unexpected query: %s", query)
	}))
	defer server.Close()

	cfg := DiscussionCommentConfig{
		Client:   ghTestClient("test-token", server.URL),
		Template: "/tmp/tekton-test-templates-github/discussion.tmpl",
	}

	handler, err := NewDiscussionCommentHandler(cfg, nil)
	if err != nil {
		t.Fatalf("NewDiscussionCommentHandler() unexpected error: %v", err)
	}

	event := domain.Event{
		Provider:         "github", //nolint:goconst
		State:            domain.StateSuccess,
		DiscussionNumber: &discussionNum,
		Repo: domain.Repo{
			Owner: testDiscussOwner,
			Name:  testDiscussRepo,
		},
		RunName: "pipeline-run-1",
	}

	err = handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
}

func TestDiscussionCommentHandler_MissingDiscussionNumber(t *testing.T) {
	cfg := DiscussionCommentConfig{
		Client: ghTestClient("test-token", "https://api.github.com"), //nolint:goconst
	}

	handler, err := NewDiscussionCommentHandler(cfg, nil)
	if err != nil {
		t.Fatalf("NewDiscussionCommentHandler() unexpected error: %v", err)
	}

	event := domain.Event{
		Provider:         "github",
		State:            domain.StateSuccess,
		DiscussionNumber: nil, // No discussion number
		Repo: domain.Repo{
			Owner: testDiscussOwner,
			Name:  testDiscussRepo,
		},
	}

	err = handler.Handle(context.Background(), event)
	if err != nil {
		t.Errorf("expected nil (skip), got error: %v", err)
	}
}

func TestDiscussionCommentHandler_StateFilter(t *testing.T) {
	discussionNum := 42
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called when CEL filter doesn't match")
	}))
	defer server.Close()

	cfg := DiscussionCommentConfig{
		Client: ghTestClient("test-token", server.URL),
	}

	innerHandler, err := NewDiscussionCommentHandler(cfg, nil)
	if err != nil {
		t.Fatalf("NewDiscussionCommentHandler() unexpected error: %v", err)
	}

	// Wrap with CEL filter: only failure states
	wrappedHandler, err := middleware.WrapWithCEL(innerHandler, `event.State == "failure"`, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to wrap with CEL: %v", err)
	}

	event := domain.Event{
		Provider:         "github",
		State:            domain.StateSuccess, // Success not in filter
		DiscussionNumber: &discussionNum,
		Repo: domain.Repo{
			Owner: testDiscussOwner,
			Name:  testDiscussRepo,
		},
	}

	err = wrappedHandler.Handle(context.Background(), event)
	if err != nil {
		t.Errorf("expected nil (skip due to state filter), got error: %v", err)
	}
}

func TestDiscussionCommentHandler_ProviderMismatch(t *testing.T) {
	discussionNum := 42
	cfg := DiscussionCommentConfig{
		Client: ghTestClient("test-token", "https://api.github.com"),
	}

	handler, err := NewDiscussionCommentHandler(cfg, nil)
	if err != nil {
		t.Fatalf("NewDiscussionCommentHandler() unexpected error: %v", err)
	}

	event := domain.Event{
		Provider:         "gitlab", //nolint:goconst // Not providerGitHub
		State:            domain.StateSuccess,
		DiscussionNumber: &discussionNum,
		Repo: domain.Repo{
			Owner: testDiscussOwner,
			Name:  testDiscussRepo,
		},
	}

	err = handler.Handle(context.Background(), event)
	if err != nil {
		t.Errorf("expected nil (skip due to provider), got error: %v", err)
	}
}

func TestDiscussionCommentHandler_NodeIDResolutionFailure(t *testing.T) {
	discussionNum := 999
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Discussions disabled or not found
		resp := map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"discussion": nil, // Not found
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := DiscussionCommentConfig{
		Client: ghTestClient("test-token", server.URL),
	}

	handler, err := NewDiscussionCommentHandler(cfg, nil)
	if err != nil {
		t.Fatalf("NewDiscussionCommentHandler() unexpected error: %v", err)
	}

	event := domain.Event{
		Provider:         "github",
		State:            domain.StateSuccess,
		DiscussionNumber: &discussionNum,
		Repo: domain.Repo{
			Owner: testDiscussOwner,
			Name:  testDiscussRepo,
		},
	}

	err = handler.Handle(context.Background(), event)
	if err == nil {
		t.Fatal("expected error when discussion not found")
	}

	if !strContains(err.Error(), "not found") && !strContains(err.Error(), "disabled") {
		t.Errorf("expected 'not found' or 'disabled' in error, got: %v", err)
	}
}

func TestDiscussionCommentHandler_MutationError(t *testing.T) {
	discussionNum := 42
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		query := req["query"].(string)

		// First call: node ID resolution succeeds
		if callCount == 0 {
			callCount++
			resp := map[string]any{
				"data": map[string]any{
					"repository": map[string]any{
						"discussion": map[string]any{
							"id": testDiscussNodeID,
						},
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Second call: mutation fails
		if strContains(query, "addDiscussionComment") {
			resp := map[string]any{
				"errors": []map[string]any{
					{
						"message": "User does not have permission to comment on this discussion",
						"type":    "FORBIDDEN",
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
	}))
	defer server.Close()

	cfg := DiscussionCommentConfig{
		Client: ghTestClient("test-token", server.URL),
	}

	handler, err := NewDiscussionCommentHandler(cfg, nil)
	if err != nil {
		t.Fatalf("NewDiscussionCommentHandler() unexpected error: %v", err)
	}

	event := domain.Event{
		Provider:         "github",
		State:            domain.StateSuccess,
		DiscussionNumber: &discussionNum,
		Repo: domain.Repo{
			Owner: testDiscussOwner,
			Name:  testDiscussRepo,
		},
		RunName: "pipeline-run-1",
	}

	err = handler.Handle(context.Background(), event)
	if err == nil {
		t.Fatal("expected error from mutation failure")
	}

	if !strContains(err.Error(), "permission") {
		t.Errorf("expected 'permission' in error, got: %v", err)
	}
}
