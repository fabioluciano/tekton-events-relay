package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// upsertMockServer simulates the GitHub issue comments API with an
// in-memory comment list, counting create and edit calls.
type upsertMockServer struct {
	mu       sync.Mutex
	comments []map[string]any
	nextID   int64
	creates  int
	edits    int
}

func (m *upsertMockServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		switch r.Method {
		case "GET":
			_ = json.NewEncoder(w).Encode(m.comments)
		case "POST": //nolint:goconst // HTTP method
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			m.nextID++
			m.creates++
			m.comments = append(m.comments, map[string]any{"id": m.nextID, "body": payload["body"]}) //nolint:goconst // test mock
			w.WriteHeader(http.StatusCreated)
		case "PATCH":
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			m.edits++
			// URL: .../issues/comments/{id}
			parts := strings.Split(r.URL.Path, "/")
			id := parts[len(parts)-1]
			for _, c := range m.comments {
				if fmt.Sprintf("%v", c["id"]) == id {
					c["body"] = payload["body"]
				}
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func upsertEvent(state domain.State) domain.Event {
	pr := 5
	return domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		RunName:  "my-run",
		RunID:    "uid-123",
		PRNumber: &pr,
		State:    state,
	}
}

func TestPRCommentHandler_UpsertEditsExistingComment(t *testing.T) {
	mock := &upsertMockServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()

	h, err := NewPRCommentHandler(PRCommentConfig{
		Client:   ghTestClient(testHandlerToken, server.URL),
		Template: "Run {{.RunName}}: {{.State}}",
		Mode:     "upsert",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}

	if err := h.Handle(context.Background(), upsertEvent(domain.StateRunning)); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	if err := h.Handle(context.Background(), upsertEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("second Handle: %v", err)
	}

	if mock.creates != 1 {
		t.Errorf("creates = %d, want 1", mock.creates)
	}
	if mock.edits != 1 {
		t.Errorf("edits = %d, want 1", mock.edits)
	}
	if len(mock.comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(mock.comments))
	}

	body := mock.comments[0]["body"].(string)
	if !strings.HasPrefix(body, "<!-- tekton-events-relay:uid-123:pr_comment -->") {
		t.Errorf("comment body missing marker: %q", body)
	}
	if !strings.Contains(body, "success") {
		t.Errorf("comment body not updated to final state: %q", body)
	}
}

func TestPRCommentHandler_UpsertDifferentRunsCreateSeparateComments(t *testing.T) {
	mock := &upsertMockServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()

	h, err := NewPRCommentHandler(PRCommentConfig{
		Client:   ghTestClient(testHandlerToken, server.URL),
		Template: "Run {{.RunName}}",
		Mode:     "upsert",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}

	e1 := upsertEvent(domain.StateSuccess)
	e2 := upsertEvent(domain.StateSuccess)
	e2.RunID = "uid-456"

	if err := h.Handle(context.Background(), e1); err != nil {
		t.Fatalf("Handle e1: %v", err)
	}
	if err := h.Handle(context.Background(), e2); err != nil {
		t.Fatalf("Handle e2: %v", err)
	}

	if mock.creates != 2 || mock.edits != 0 {
		t.Errorf("creates = %d edits = %d, want 2 creates 0 edits", mock.creates, mock.edits)
	}
}

func TestPRCommentHandler_UpsertListFailureFallsBackToCreate(t *testing.T) {
	var creates int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		creates++
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	h, err := NewPRCommentHandler(PRCommentConfig{
		Client:   ghTestClient(testHandlerToken, server.URL),
		Template: "msg",
		Mode:     "upsert",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}

	if err := h.Handle(context.Background(), upsertEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if creates != 1 {
		t.Errorf("creates = %d, want 1 (fallback to create)", creates)
	}
}

func TestPRCommentHandler_InvalidModeRejected(t *testing.T) {
	_, err := NewPRCommentHandler(PRCommentConfig{Client: ghTestClient(testHandlerToken, ""), Mode: "replace"}, zap.NewNop())
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestPRCommentHandler_DefaultModeCreatesEachTime(t *testing.T) {
	mock := &upsertMockServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()

	h, err := NewPRCommentHandler(PRCommentConfig{
		Client:   ghTestClient(testHandlerToken, server.URL),
		Template: "msg",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewPRCommentHandler() unexpected error: %v", err)
	}

	for range 2 {
		if err := h.Handle(context.Background(), upsertEvent(domain.StateSuccess)); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}
	if mock.creates != 2 || mock.edits != 0 {
		t.Errorf("creates = %d edits = %d, want 2 creates 0 edits (create mode)", mock.creates, mock.edits)
	}
}
