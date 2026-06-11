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

// notesMock simulates the GitLab MR notes API.
type notesMock struct {
	mu      sync.Mutex
	notes   []map[string]any
	nextID  int64
	creates int
	updates int
}

func (m *notesMock) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if !strings.Contains(r.URL.Path, "/notes") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(m.notes)
		case http.MethodPost:
			var p map[string]string
			_ = json.NewDecoder(r.Body).Decode(&p)
			m.nextID++
			m.creates++
			m.notes = append(m.notes, map[string]any{"id": m.nextID, "body": p["body"]})
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(m.notes[len(m.notes)-1])
		case http.MethodPut:
			var p map[string]string
			_ = json.NewDecoder(r.Body).Decode(&p)
			m.updates++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": m.nextID, "body": p["body"]})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func newMRHandler(t *testing.T, baseURL, mode string) notifier.ActionHandler {
	t.Helper()
	h, err := NewMRCommentHandler(MRCommentConfig{
		Token: "token", BaseURL: baseURL, Name: "gitlab-main",
		Template: "Run {{.RunName}}: {{.State}}", Mode: mode, Log: zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewMRCommentHandler: %v", err)
	}
	return h
}

func mrEvent(state domain.State) domain.Event {
	pr := 7
	return domain.Event{
		Provider: "gitlab-main",
		Repo:     domain.Repo{ID: "42"},
		RunName:  "run-1", RunID: "uid-123",
		PRNumber: &pr, State: state,
	}
}

func TestMRCommentHandler_CreatesNote(t *testing.T) {
	mock := &notesMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newMRHandler(t, srv.URL, "")
	if err := h.Handle(context.Background(), mrEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if mock.creates != 1 {
		t.Errorf("creates = %d, want 1", mock.creates)
	}
}

func TestMRCommentHandler_UpsertEditsExistingNote(t *testing.T) {
	mock := &notesMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newMRHandler(t, srv.URL, "upsert")
	if err := h.Handle(context.Background(), mrEvent(domain.StateRunning)); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	if err := h.Handle(context.Background(), mrEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	if mock.creates != 1 || mock.updates != 1 {
		t.Errorf("creates=%d updates=%d, want 1/1", mock.creates, mock.updates)
	}
	if body := mock.notes[0]["body"].(string); !strings.HasPrefix(body, "<!-- tekton-events-relay:uid-123:pr_comment -->") {
		t.Errorf("missing marker: %q", body)
	}
}

func TestMRCommentHandler_Skips(t *testing.T) {
	mock := &notesMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newMRHandler(t, srv.URL, "")
	e := mrEvent(domain.StateSuccess)
	e.Provider = "github"
	_ = h.Handle(context.Background(), e)

	e = mrEvent(domain.StateSuccess)
	e.PRNumber = nil
	_ = h.Handle(context.Background(), e)

	e = mrEvent(domain.StateSuccess)
	e.Repo = domain.Repo{}
	_ = h.Handle(context.Background(), e)

	if mock.creates != 0 {
		t.Errorf("creates = %d, want 0", mock.creates)
	}
}

func TestMRCommentHandler_InvalidModeRejected(t *testing.T) {
	_, err := NewMRCommentHandler(MRCommentConfig{Token: "t", Name: "g", Mode: "replace", Log: zap.NewNop()})
	if err == nil {
		t.Fatal("expected error")
	}
}
