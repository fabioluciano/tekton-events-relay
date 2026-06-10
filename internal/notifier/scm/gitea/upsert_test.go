package gitea

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// giteaMock simulates the subset of the Gitea API used by comment upsert.
type giteaMock struct {
	mu       sync.Mutex
	comments []map[string]any
	nextID   int64
	creates  int
	edits    int
}

func (m *giteaMock) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		switch {
		case r.URL.Path == "/api/v1/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "1.22.0"})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/comments"):
			_ = json.NewEncoder(w).Encode(m.comments)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/comments"):
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			m.nextID++
			m.creates++
			m.comments = append(m.comments, map[string]any{"id": m.nextID, "body": payload["body"]})
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": m.nextID, "body": payload["body"]})
		case r.Method == "PATCH":
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			m.edits++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": m.nextID, "body": payload["body"]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestPRCommentHandler_UpsertEditsExistingComment(t *testing.T) {
	mock := &giteaMock{}
	server := httptest.NewServer(mock.handler())
	defer server.Close()

	h, err := NewPRCommentHandler(PRCommentConfig{
		Token:    "token",
		BaseURL:  server.URL,
		Template: "Run {{.RunName}}: {{.State}}",
		Mode:     "upsert",
		Log:      zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewPRCommentHandler: %v", err)
	}

	pr := 5
	event := domain.Event{
		Provider: providerGitea,
		Repo:     domain.Repo{Owner: "org", Name: "repo"},
		RunName:  "my-run",
		RunID:    "uid-123",
		PRNumber: &pr,
		State:    domain.StateRunning,
	}

	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	event.State = domain.StateSuccess
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatalf("second Handle: %v", err)
	}

	if mock.creates != 1 {
		t.Errorf("creates = %d, want 1", mock.creates)
	}
	if mock.edits != 1 {
		t.Errorf("edits = %d, want 1", mock.edits)
	}
}

func TestPRCommentHandler_InvalidModeRejected(t *testing.T) {
	_, err := NewPRCommentHandler(PRCommentConfig{Token: "t", BaseURL: "http://localhost", Mode: "replace", Log: zap.NewNop()})
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}
