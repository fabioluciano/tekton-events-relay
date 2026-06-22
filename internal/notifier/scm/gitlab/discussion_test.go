package gitlab

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
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// discussionsMock simulates the GitLab MR discussions API.
type discussionsMock struct {
	mu          sync.Mutex
	discussions []map[string]any
	nextID      int64
	creates     int
	resolves    int
}

func (m *discussionsMock) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if !strings.Contains(r.URL.Path, "/discussions") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// A trailing segment after /discussions/ means a specific discussion (resolve).
		resolvePath := strings.Contains(r.URL.Path, "/discussions/")
		switch {
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(m.discussions)
		case r.Method == http.MethodPost && !resolvePath:
			var p map[string]string
			_ = json.NewDecoder(r.Body).Decode(&p)
			m.nextID++
			m.creates++
			d := map[string]any{
				"id":              "disc-id",
				"individual_note": false,
				"notes": []map[string]any{
					{"id": m.nextID, ccBodyKey: p[ccBodyKey], "resolvable": true, "resolved": false},
				},
			}
			m.discussions = append(m.discussions, d)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(d)
		case r.Method == http.MethodPut && resolvePath:
			m.resolves++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "disc-id"})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func newDiscussionHandler(t *testing.T, baseURL string) notifier.ActionHandler {
	t.Helper()
	client, err := NewClient("token", baseURL, false, false, zap.NewNop())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	h, err := NewDiscussionHandler(DiscussionConfig{
		Client: client, Name: ccProvider,
		Template: "Run {{.RunName}}: {{.State}}", Log: zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewDiscussionHandler: %v", err)
	}
	return h
}

func discussionEvent(state domain.State) domain.Event {
	pr := 7
	return domain.Event{
		Provider: ccProvider,
		Repo:     domain.Repo{ID: "42"},
		RunName:  "run-1", RunID: "uid-123",
		PRNumber: &pr, State: state,
	}
}

func TestDiscussionHandler_NameAndType(t *testing.T) {
	h := newDiscussionHandler(t, "")
	if h.Name() != ccProvider {
		t.Errorf("Name = %q, want %q", h.Name(), ccProvider)
	}
	if h.Type() != notifier.ActionDiscussionComment {
		t.Errorf("Type = %q, want %q", h.Type(), notifier.ActionDiscussionComment)
	}
}

func TestDiscussionHandler_OpensThreadOnNonTerminal(t *testing.T) {
	tests := []struct {
		name  string
		state domain.State
	}{
		{"pending", domain.StatePending},
		{"running", domain.StateRunning},
		{"failure keeps thread open", domain.StateFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &discussionsMock{}
			srv := httptest.NewServer(mock.handler())
			defer srv.Close()

			h := newDiscussionHandler(t, srv.URL)
			if err := h.Handle(context.Background(), discussionEvent(tt.state)); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if mock.creates != 1 {
				t.Errorf("creates = %d, want 1", mock.creates)
			}
			if mock.resolves != 0 {
				t.Errorf("resolves = %d, want 0", mock.resolves)
			}
		})
	}
}

func TestDiscussionHandler_ResolvesExistingOnSuccess(t *testing.T) {
	mock := &discussionsMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newDiscussionHandler(t, srv.URL)
	// First a non-terminal event opens the thread.
	if err := h.Handle(context.Background(), discussionEvent(domain.StateRunning)); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	// Then success resolves the same thread without creating a new one.
	if err := h.Handle(context.Background(), discussionEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	if mock.creates != 1 {
		t.Errorf("creates = %d, want 1", mock.creates)
	}
	if mock.resolves != 1 {
		t.Errorf("resolves = %d, want 1", mock.resolves)
	}
	if body := mock.discussions[0]["notes"].([]map[string]any)[0]["body"].(string); !strings.HasPrefix(body, "<!-- tekton-events-relay:uid-123:discussion_comment -->") {
		t.Errorf("missing marker: %q", body)
	}
}

func TestDiscussionHandler_SuccessCreatesThenResolvesWhenAbsent(t *testing.T) {
	mock := &discussionsMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newDiscussionHandler(t, srv.URL)
	// Only a terminal success observed: thread must be created then resolved.
	if err := h.Handle(context.Background(), discussionEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if mock.creates != 1 {
		t.Errorf("creates = %d, want 1", mock.creates)
	}
	if mock.resolves != 1 {
		t.Errorf("resolves = %d, want 1", mock.resolves)
	}
}

func TestDiscussionHandler_OpenIsIdempotent(t *testing.T) {
	mock := &discussionsMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newDiscussionHandler(t, srv.URL)
	for range 3 {
		if err := h.Handle(context.Background(), discussionEvent(domain.StateRunning)); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}
	if mock.creates != 1 {
		t.Errorf("creates = %d, want 1 (idempotent open)", mock.creates)
	}
}

func TestDiscussionHandler_Skips(t *testing.T) {
	mock := &discussionsMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newDiscussionHandler(t, srv.URL)

	// Wrong provider.
	e := discussionEvent(domain.StateSuccess)
	e.Provider = ccForeign
	_ = h.Handle(context.Background(), e)

	// No MR IID (MR-only: issue discussions are not resolvable).
	e = discussionEvent(domain.StateSuccess)
	e.PRNumber = nil
	_ = h.Handle(context.Background(), e)

	// Unidentifiable project.
	e = discussionEvent(domain.StateSuccess)
	e.Repo = domain.Repo{}
	_ = h.Handle(context.Background(), e)

	if mock.creates != 0 {
		t.Errorf("creates = %d, want 0", mock.creates)
	}
	if mock.resolves != 0 {
		t.Errorf("resolves = %d, want 0", mock.resolves)
	}
}
