package incidentio

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	testName   = "test"
	testAPIKey = "key"
)

type fakeToken struct {
	val string
	err error
}

func (f *fakeToken) Token(_ context.Context) (string, error) { return f.val, f.err }

func TestNotifier_Name(t *testing.T) {
	n := New(Config{
		Name:   "my-incidentio",
		APIKey: &fakeToken{val: testAPIKey},
	}, zap.NewNop())
	if n.Name() != "my-incidentio" {
		t.Fatalf("Name() = %q, want %q", n.Name(), "my-incidentio")
	}
}

func TestNotifier_Type(t *testing.T) {
	n := New(Config{
		Name:   testName,
		APIKey: &fakeToken{val: testAPIKey},
	}, zap.NewNop())
	if n.Type() != "notify" {
		t.Fatalf("Type() = %q, want %q", n.Type(), "notify")
	}
}

func TestNotifier_Close(t *testing.T) {
	n := New(Config{
		Name:   testName,
		APIKey: &fakeToken{val: testAPIKey},
	}, zap.NewNop())
	if err := n.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
}

func TestNotifier_Handle_SkipsNonFailureStates(t *testing.T) {
	n := New(Config{
		Name:   testName,
		APIKey: &fakeToken{val: testAPIKey},
	}, zap.NewNop())

	states := []domain.State{
		domain.StatePending,
		domain.StateRunning,
		domain.StateSuccess,
	}

	for _, s := range states {
		t.Run(string(s), func(t *testing.T) {
			err := n.Handle(context.Background(), domain.Event{
				State:   s,
				RunName: "test-run",
				RunID:   "run-123",
			})
			if err != nil {
				t.Fatalf("Handle(%s) should return nil, got: %v", s, err)
			}
		})
	}
}

func TestNotifier_Handle_CreatesIncidentOnFailure(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("unmarshal body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"incident":{"id":"inc-123"}}`))
	}))
	defer srv.Close()

	n := New(Config{
		Name:   testName,
		APIKey: &fakeToken{val: "test-api-key"},
	}, zap.NewNop())
	n.base.BuildURL = func(_ domain.Event) (string, error) { return srv.URL, nil }

	err := n.Handle(context.Background(), domain.Event{
		State:        domain.StateFailure,
		RunName:      "my-pipeline-run",
		RunID:        "uid-abc",
		Namespace:    "default",
		PipelineName: "my-pipeline",
	})
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}

	if gotAuth != "Bearer test-api-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-api-key")
	}
	if gotBody["name"] != "Pipeline my-pipeline-run failed" {
		t.Errorf("name = %v, want %v", gotBody["name"], "Pipeline my-pipeline-run failed")
	}
	if gotBody["idempotency_key"] != "tekton-relay:uid-abc" {
		t.Errorf("idempotency_key = %v, want %v", gotBody["idempotency_key"], "tekton-relay:uid-abc")
	}
	if gotBody["visibility"] != "public" {
		t.Errorf("visibility = %v, want %v", gotBody["visibility"], "public")
	}
	if gotBody["mode"] != "standard" {
		t.Errorf("mode = %v, want %v", gotBody["mode"], "standard")
	}
}

func TestNotifier_Handle_CreatesIncidentOnError(t *testing.T) {
	var called atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"incident":{"id":"inc-456"}}`))
	}))
	defer srv.Close()

	n := New(Config{
		Name:   testName,
		APIKey: &fakeToken{val: testAPIKey},
	}, zap.NewNop())
	n.base.BuildURL = func(_ domain.Event) (string, error) { return srv.URL, nil }

	err := n.Handle(context.Background(), domain.Event{
		State:   domain.StateError,
		RunName: "run",
		RunID:   "uid",
	})
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if !called.Load() {
		t.Error("expected HTTP call was not made for error state")
	}
}

func TestNotifier_Handle_SetsVisibility(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"incident":{"id":"inc-789"}}`))
	}))
	defer srv.Close()

	n := New(Config{
		Name:       "test",
		APIKey:     &fakeToken{val: "key"},
		Visibility: "private",
	}, zap.NewNop())
	n.base.BuildURL = func(_ domain.Event) (string, error) { return srv.URL, nil }

	err := n.Handle(context.Background(), domain.Event{
		State:   domain.StateFailure,
		RunName: "run",
		RunID:   "uid",
	})
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if gotBody["visibility"] != "private" {
		t.Errorf("visibility = %v, want %v", gotBody["visibility"], "private")
	}
}

func TestNotifier_Handle_IncludesSeverityAndType(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"incident":{"id":"inc-abc"}}`))
	}))
	defer srv.Close()

	n := New(Config{
		Name:           "test",
		APIKey:         &fakeToken{val: "key"},
		SeverityID:     "sev-123",
		IncidentTypeID: "type-456",
	}, zap.NewNop())
	n.base.BuildURL = func(_ domain.Event) (string, error) { return srv.URL, nil }

	err := n.Handle(context.Background(), domain.Event{
		State:   domain.StateFailure,
		RunName: "run",
		RunID:   "uid",
	})
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if gotBody["severity_id"] != "sev-123" {
		t.Errorf("severity_id = %v, want %v", gotBody["severity_id"], "sev-123")
	}
	if gotBody["incident_type_id"] != "type-456" {
		t.Errorf("incident_type_id = %v, want %v", gotBody["incident_type_id"], "type-456")
	}
}

func TestNotifier_Handle_WithoutOptionalFields(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"incident":{"id":"inc-def"}}`))
	}))
	defer srv.Close()

	n := New(Config{
		Name:   testName,
		APIKey: &fakeToken{val: testAPIKey},
	}, zap.NewNop())
	n.base.BuildURL = func(_ domain.Event) (string, error) { return srv.URL, nil }

	err := n.Handle(context.Background(), domain.Event{
		State:   domain.StateFailure,
		RunName: "run",
		RunID:   "uid",
	})
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if _, ok := gotBody["severity_id"]; ok {
		t.Error("severity_id should not be present when not configured")
	}
	if _, ok := gotBody["incident_type_id"]; ok {
		t.Error("incident_type_id should not be present when not configured")
	}
}

func TestNotifier_Handle_DefaultVisibility(t *testing.T) {
	n := New(Config{
		Name:   testName,
		APIKey: &fakeToken{val: testAPIKey},
	}, zap.NewNop())
	if n.cfg.Visibility != "public" {
		t.Errorf("default visibility = %q, want %q", n.cfg.Visibility, "public")
	}
}

func TestNotifier_Payload(t *testing.T) {
	n := New(Config{
		Name:   testName,
		APIKey: &fakeToken{val: testAPIKey},
	}, zap.NewNop())

	e := domain.Event{
		State:        domain.StateFailure,
		RunName:      "pipeline-run-1",
		RunID:        "uid-xyz",
		Namespace:    "ci",
		PipelineName: "build-and-test",
	}

	payload, err := n.payload(e)
	if err != nil {
		t.Fatalf("payload() error: %v", err)
	}
	m, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not map[string]any")
	}
	if m["name"] != "Pipeline pipeline-run-1 failed" {
		t.Errorf("name = %v", m["name"])
	}
	if m["idempotency_key"] != "tekton-relay:uid-xyz" {
		t.Errorf("idempotency_key = %v", m["idempotency_key"])
	}
	if m["visibility"] != "public" {
		t.Errorf("visibility = %v", m["visibility"])
	}
	if m["mode"] != "standard" {
		t.Errorf("mode = %v", m["mode"])
	}
}
