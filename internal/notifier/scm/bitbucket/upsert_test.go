package bitbucket

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

// cloudMock simulates the Bitbucket Cloud PR comments API.
type cloudMock struct {
	mu       sync.Mutex
	comments []map[string]any
	nextID   int64
	creates  int
	updates  int
}

func (m *cloudMock) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		switch r.Method {
		case "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"values": m.comments})
		case "POST":
			var payload map[string]map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			m.nextID++
			m.creates++
			m.comments = append(m.comments, map[string]any{
				"id":      m.nextID,
				"content": map[string]string{"raw": payload["content"]["raw"]},
			})
			w.WriteHeader(http.StatusCreated)
		case "PUT":
			m.updates++
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func TestCloudCommentHandler_UpsertEditsExistingComment(t *testing.T) {
	mock := &cloudMock{}
	server := httptest.NewServer(mock.handler())
	defer server.Close()

	h, err := NewCloudCommentHandler(CloudCommentConfig{
		Username:    "user",
		AppPassword: "pass",
		BaseURL:     server.URL,
		Template:    "Run {{.RunName}}: {{.State}}",
		Mode:        "upsert",
		Log:         zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewCloudCommentHandler: %v", err)
	}

	pr := 7
	event := domain.Event{
		Provider: providerCloud,
		Repo:     domain.Repo{Workspace: "ws", Name: testRepoName},
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
	if mock.updates != 1 {
		t.Errorf("updates = %d, want 1", mock.updates)
	}

	raw := mock.comments[0]["content"].(map[string]string)["raw"]
	if !strings.HasPrefix(raw, "<!-- tekton-events-relay:uid-123:pr_comment -->") {
		t.Errorf("comment missing marker: %q", raw)
	}
}

func TestCloudCommentHandler_InvalidModeRejected(t *testing.T) {
	_, err := NewCloudCommentHandler(CloudCommentConfig{
		Username: "u", AppPassword: "p", BaseURL: "http://localhost", Mode: "replace", Log: zap.NewNop(),
	})
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}
