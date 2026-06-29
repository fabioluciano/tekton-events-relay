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

// incidentMock simulates the GitLab Issues API for incident_create tests.
type incidentMock struct {
	mu       sync.Mutex
	issues   []map[string]any
	nextIID  int64
	creates  int
	updates  int
	lists    int
	failOnce bool // simulate issue_type=incident unsupported on first create
}

func (m *incidentMock) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/issues"):
			m.handleCreate(w, r)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/issues"):
			m.handleList(w, r)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/issues/"):
			m.handleUpdate(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (m *incidentMock) handleCreate(w http.ResponseWriter, r *http.Request) {
	var p map[string]any
	_ = json.NewDecoder(r.Body).Decode(&p)

	// Simulate issue_type=incident unsupported (GitLab < 14.0).
	if m.failOnce {
		if t, ok := p["issue_type"].(string); ok && t == "incident" {
			m.failOnce = false
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "issue_type is invalid"})
			return
		}
	}

	m.nextIID++
	var labels []string
	if rawLabels, ok := p["labels"]; ok && rawLabels != nil {
		switch v := rawLabels.(type) {
		case []any:
			for _, l := range v {
				labels = append(labels, fmt.Sprint(l))
			}
		case string:
			labels = strings.Split(v, ",")
		}
	}
	issue := map[string]any{
		"id":          m.nextIID * 100, // SDK needs numeric id for UnmarshalJSON
		"iid":         m.nextIID,
		"project_id":  42,
		"title":       p["title"],
		"description": p["description"],
		"labels":      labels,
		"issue_type":  p["issue_type"],
		"state":       "opened",
	}
	m.issues = append(m.issues, issue)
	m.creates++
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(issue)
}

func (m *incidentMock) handleList(w http.ResponseWriter, _ *http.Request) {
	m.lists++
	// Return all issues (the handler filters by label).
	_ = json.NewEncoder(w).Encode(m.issues)
}

func (m *incidentMock) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var p map[string]any
	_ = json.NewDecoder(r.Body).Decode(&p)

	// Extract IID from URL path.
	parts := strings.Split(r.URL.Path, "/")
	var iid int64
	for i, part := range parts {
		if part == "issues" && i+1 < len(parts) {
			_, _ = fmt.Sscanf(parts[i+1], "%d", &iid)
			break
		}
	}

	for _, issue := range m.issues {
		if issue["iid"] == iid {
			if se, ok := p["state_event"].(string); ok && se == "close" {
				issue["state"] = "closed"
			}
			m.updates++
			_ = json.NewEncoder(w).Encode(issue)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func newIncidentHandler(t *testing.T, baseURL string) notifier.ActionHandler {
	t.Helper()
	client, err := NewClient("token", baseURL, false, false, zap.NewNop())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	h, err := NewIncidentCreateHandler(IncidentCreateConfig{
		Client: client, Name: ccProvider, Log: zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewIncidentCreateHandler: %v", err)
	}
	return h
}

func incidentEvent(state domain.State) domain.Event {
	return domain.Event{
		Provider: ccProvider,
		Repo:     domain.Repo{ID: "42"},
		RunName:  "pipeline-run-1",
		RunID:    "uid-abc-123",
		State:    state,
	}
}

func TestIncidentCreate_Type(t *testing.T) {
	h := newIncidentHandler(t, "")
	if got := h.Type(); got != notifier.ActionIncidentCreate {
		t.Errorf("Type() = %q, want %q", got, notifier.ActionIncidentCreate)
	}
	if got := h.Name(); got != ccProvider {
		t.Errorf("Name() = %q, want %q", got, ccProvider)
	}
}

func TestIncidentCreate_CreatesOnFailure(t *testing.T) {
	mock := &incidentMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newIncidentHandler(t, srv.URL)
	if err := h.Handle(context.Background(), incidentEvent(domain.StateFailure)); err != nil {
		t.Fatalf("Handle failure: %v", err)
	}
	if mock.creates != 1 {
		t.Fatalf("creates = %d, want 1", mock.creates)
	}
	if mock.issues[0]["issue_type"] != "incident" {
		t.Errorf("issue_type = %v, want incident", mock.issues[0]["issue_type"])
	}
	labels, ok := mock.issues[0]["labels"].([]string)
	if !ok || len(labels) != 1 || labels[0] != "tekton-relay:uid-abc-123" {
		t.Errorf("labels = %v, want [tekton-relay:uid-abc-123]", mock.issues[0]["labels"])
	}
}

func TestIncidentCreate_CreatesOnError(t *testing.T) {
	mock := &incidentMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newIncidentHandler(t, srv.URL)
	if err := h.Handle(context.Background(), incidentEvent(domain.StateError)); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if mock.creates != 1 {
		t.Fatalf("creates = %d, want 1", mock.creates)
	}
	title, ok := mock.issues[0]["title"].(string)
	if !ok || !strings.Contains(title, "Pipeline error") {
		t.Errorf("title = %q, want containing 'Pipeline error'", title)
	}
}

func TestIncidentDedup_SkipsExistingIncident(t *testing.T) {
	mock := &incidentMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newIncidentHandler(t, srv.URL)

	// First call creates the incident.
	if err := h.Handle(context.Background(), incidentEvent(domain.StateFailure)); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	if mock.creates != 1 {
		t.Fatalf("creates after first = %d, want 1", mock.creates)
	}

	// Second call with same RunID should NOT create a duplicate.
	if err := h.Handle(context.Background(), incidentEvent(domain.StateFailure)); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	if mock.creates != 1 {
		t.Fatalf("creates after second = %d, want 1 (dedup)", mock.creates)
	}
	if mock.lists < 2 {
		t.Errorf("lists = %d, want >= 2 (search before each create)", mock.lists)
	}
}

func TestIncidentClose_ClosesOnSuccess(t *testing.T) {
	mock := &incidentMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newIncidentHandler(t, srv.URL)

	// Create incident on failure.
	if err := h.Handle(context.Background(), incidentEvent(domain.StateFailure)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if mock.creates != 1 {
		t.Fatalf("creates = %d, want 1", mock.creates)
	}
	if mock.issues[0]["state"] != "opened" {
		t.Fatalf("state = %v, want opened", mock.issues[0]["state"])
	}

	// Close incident on success.
	if err := h.Handle(context.Background(), incidentEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("close: %v", err)
	}
	if mock.updates != 1 {
		t.Fatalf("updates = %d, want 1", mock.updates)
	}
	if mock.issues[0]["state"] != "closed" {
		t.Errorf("state = %v, want closed", mock.issues[0]["state"])
	}
}

func TestIncidentClose_NoOpWhenNoIncident(t *testing.T) {
	mock := &incidentMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newIncidentHandler(t, srv.URL)

	// Success without a prior failure — should be a no-op.
	if err := h.Handle(context.Background(), incidentEvent(domain.StateSuccess)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if mock.updates != 0 {
		t.Errorf("updates = %d, want 0 (no incident to close)", mock.updates)
	}
}

func TestIncidentCreate_FallbackToIssueType(t *testing.T) {
	// First create with issue_type=incident fails (GitLab < 14.0).
	mock := &incidentMock{failOnce: true}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	h := newIncidentHandler(t, srv.URL)
	if err := h.Handle(context.Background(), incidentEvent(domain.StateFailure)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if mock.creates != 1 {
		t.Fatalf("creates = %d, want 1 (fallback)", mock.creates)
	}
	if mock.issues[0]["issue_type"] != "issue" {
		t.Errorf("issue_type = %v, want issue (fallback)", mock.issues[0]["issue_type"])
	}
}

func TestIncidentCreate_Skips(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.Event)
	}{
		{"wrong provider", func(e *domain.Event) { e.Provider = ccForeign }},
		{"no project identifier", func(e *domain.Event) { e.Repo = domain.Repo{} }},
		{"pending state", func(e *domain.Event) { e.State = domain.StatePending }},
		{"running state", func(e *domain.Event) { e.State = domain.StateRunning }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &incidentMock{}
			srv := httptest.NewServer(mock.handler())
			defer srv.Close()

			h := newIncidentHandler(t, srv.URL)
			e := incidentEvent(domain.StateFailure)
			tt.mutate(&e)
			_ = h.Handle(context.Background(), e)

			if mock.creates != 0 {
				t.Errorf("creates = %d, want 0", mock.creates)
			}
		})
	}
}

func TestIncidentCreate_WithTemplate(t *testing.T) {
	mock := &incidentMock{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client, err := NewClient("token", srv.URL, false, false, zap.NewNop())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	h, err := NewIncidentCreateHandler(IncidentCreateConfig{
		Client:   client,
		Name:     ccProvider,
		Template: "Pipeline {{.RunName}} failed in {{.Namespace}}",
		Log:      zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewIncidentCreateHandler: %v", err)
	}

	e := incidentEvent(domain.StateFailure)
	e.Namespace = "production"
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if mock.creates != 1 {
		t.Fatalf("creates = %d, want 1", mock.creates)
	}
	desc, ok := mock.issues[0]["description"].(string)
	if !ok || !strings.Contains(desc, "pipeline-run-1") || !strings.Contains(desc, "production") {
		t.Errorf("description = %q, want containing run name and namespace", desc)
	}
}
