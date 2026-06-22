package gitlab

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
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	ccProvider = "gitlab-main"
	ccForeign  = "github"
	ccBodyKey  = "body"
)

// commitCommentMock simulates the GitLab commit comments + discussions APIs.
type commitCommentMock struct {
	mu          sync.Mutex
	discussions []map[string]any
	nextNote    int64
	nextDisc    int64
	comments    int
	discCreates int
	updates     int
}

func (m *commitCommentMock) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch {
		case strings.Contains(r.URL.Path, "/discussions"):
			m.serveDiscussions(w, r)
		case strings.Contains(r.URL.Path, "/comments"):
			m.comments++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"note": "ok"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (m *commitCommentMock) serveDiscussions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(m.discussions)
	case http.MethodPost:
		var p map[string]string
		_ = json.NewDecoder(r.Body).Decode(&p)
		m.nextDisc++
		m.nextNote++
		m.discCreates++
		disc := map[string]any{
			"id":    fmt.Sprintf("disc-%d", m.nextDisc),
			"notes": []map[string]any{{"id": m.nextNote, ccBodyKey: p[ccBodyKey]}},
		}
		m.discussions = append(m.discussions, disc)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(disc)
	case http.MethodPut:
		var p map[string]string
		_ = json.NewDecoder(r.Body).Decode(&p)
		m.updates++
		_ = json.NewEncoder(w).Encode(map[string]any{"id": m.nextNote, ccBodyKey: p[ccBodyKey]})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func newCommitCommentHandler(t *testing.T, baseURL, mode string) notifier.ActionHandler {
	t.Helper()
	client, err := NewClient("token", baseURL, false, false, zap.NewNop())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	h, err := NewCommitCommentHandler(CommitCommentConfig{
		Client: client, Name: ccProvider,
		Template: "Run {{.RunName}}: {{.State}}", Mode: mode, Log: zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewCommitCommentHandler: %v", err)
	}
	return h
}

func commitEvent(state domain.State) domain.Event {
	return domain.Event{
		Provider:  ccProvider,
		Repo:      domain.Repo{ID: "42"},
		RunName:   "run-1",
		RunID:     "uid-123",
		CommitSHA: "abc123",
		State:     state,
	}
}

func TestCommitCommentHandler_CreatesComment(t *testing.T) {
	mock := &commitCommentMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newCommitCommentHandler(t, srv.URL, "")
	if err := h.Handle(context.Background(), commitEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if mock.comments != 1 {
		t.Errorf("comments = %d, want 1", mock.comments)
	}
	if mock.discCreates != 0 {
		t.Errorf("discCreates = %d, want 0 (default mode must not touch discussions)", mock.discCreates)
	}
}

func TestCommitCommentHandler_UpsertEditsExistingNote(t *testing.T) {
	mock := &commitCommentMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newCommitCommentHandler(t, srv.URL, "upsert")
	if err := h.Handle(context.Background(), commitEvent(domain.StateRunning)); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	if err := h.Handle(context.Background(), commitEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	if mock.discCreates != 1 || mock.updates != 1 {
		t.Errorf("discCreates=%d updates=%d, want 1/1", mock.discCreates, mock.updates)
	}
	if mock.comments != 0 {
		t.Errorf("comments = %d, want 0 (upsert must not use the comments endpoint)", mock.comments)
	}
	notes := mock.discussions[0]["notes"].([]map[string]any)
	if body := notes[0][ccBodyKey].(string); !strings.HasPrefix(body, "<!-- tekton-events-relay:uid-123:commit_comment -->") {
		t.Errorf("missing marker: %q", body)
	}
}

func TestCommitCommentHandler_Skips(t *testing.T) {
	mock := &commitCommentMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newCommitCommentHandler(t, srv.URL, "")

	e := commitEvent(domain.StateSuccess)
	e.Provider = ccForeign
	_ = h.Handle(context.Background(), e)

	e = commitEvent(domain.StateSuccess)
	e.CommitSHA = ""
	_ = h.Handle(context.Background(), e)

	e = commitEvent(domain.StateSuccess)
	e.Repo = domain.Repo{}
	_ = h.Handle(context.Background(), e)

	if mock.comments != 0 {
		t.Errorf("comments = %d, want 0", mock.comments)
	}
}

func TestCommitCommentHandler_InvalidModeRejected(t *testing.T) {
	client, err := NewClient("t", "", false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewCommitCommentHandler(CommitCommentConfig{Client: client, Name: "g", Mode: "replace", Log: zap.NewNop()})
	if err == nil {
		t.Fatal("expected error")
	}
}
