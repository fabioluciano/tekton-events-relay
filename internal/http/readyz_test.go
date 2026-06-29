package http

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
	"github.com/fabioluciano/tekton-events-relay/internal/store"
)

type fakeSource struct{ names []string }

func (f fakeSource) Names() []string { return f.names }

func TestReadyz_JSONWithHandlerStatus(t *testing.T) {
	tracker := pipeline.NewStatusTracker()
	tracker.Observe("github", nil)
	tracker.Observe("github", errors.New("api 502"))
	tracker.Observe("slack", nil)

	health := buildHealthHandler(fakeSource{[]string{"github", "slack"}}, fakeSource{[]string{"taskrun"}}, tracker, nil) //nolint:goconst // test fixture

	rec := httptest.NewRecorder()
	health.readyEndpoint(rec, httptest.NewRequest("GET", "/readyz", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Status   string                            `json:"status"`
		Handlers map[string]pipeline.HandlerStatus `json:"handlers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	gh := body.Handlers["github"]
	if gh.Succeeded != 1 || gh.Failed != 1 || gh.LastError != "api 502" {
		t.Errorf("github status = %+v, want 1 success, 1 failure, last error recorded", gh)
	}
	if body.Handlers["slack"].Succeeded != 1 {
		t.Errorf("slack status = %+v, want 1 success", body.Handlers["slack"])
	}
}

func TestReadyz_WithMemoryStoreHealthy(t *testing.T) {
	registry := fakeSource{[]string{"github"}}
	decoders := fakeSource{[]string{"taskrun"}}

	memStore, err := store.New(config.StoreConfig{}, store.Options{})
	if err != nil {
		t.Fatalf("new memory store: %v", err)
	}
	health := buildHealthHandler(registry, decoders, nil, memStore)

	rec := httptest.NewRecorder()
	health.readyEndpoint(rec, httptest.NewRequest("GET", "/readyz", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Status string            `json:"status"`
		Store  map[string]string `json:"store"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.Store == nil {
		t.Fatal("expected store block in response")
	}
	if body.Store["status"] != "healthy" {
		t.Errorf("store status = %q, want healthy", body.Store["status"])
	}
	if body.Store["backend"] != "memory" {
		t.Errorf("store backend = %q, want memory", body.Store["backend"])
	}
}

func TestReadyz_UnavailableWithoutHandlers(t *testing.T) {
	health := buildHealthHandler(fakeSource{nil}, fakeSource{[]string{"taskrun"}}, nil, nil)

	rec := httptest.NewRecorder()
	health.readyEndpoint(rec, httptest.NewRequest("GET", "/readyz", nil))

	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "unavailable" || body.Reason == "" {
		t.Errorf("body = %+v, want unavailable with reason", body)
	}
}

func TestReadyzDegradedReturns200(t *testing.T) {
	tracker := pipeline.NewStatusTracker()

	for i := 0; i < 85; i++ {
		tracker.Observe("github", nil)
	}
	for i := 0; i < 15; i++ {
		tracker.Observe("github", errors.New("api error"))
	}

	health := buildHealthHandler(
		fakeSource{[]string{"github"}},
		fakeSource{[]string{"taskrun"}},
		tracker,
		nil,
	)

	rec := httptest.NewRecorder()
	health.readyEndpoint(rec, httptest.NewRequest("GET", "/readyz", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (degraded does not trigger 503)", rec.Code)
	}

	var body struct {
		Status   string                            `json:"status"`
		Handlers map[string]pipeline.HandlerStatus `json:"handlers"`
		Degraded map[string]float64                `json:"degraded"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.Degraded == nil {
		t.Fatal("expected degraded field in response")
	}
	rate, ok := body.Degraded["github"]
	if !ok {
		t.Fatal("expected 'github' in degraded map")
	}
	if rate <= 0.10 {
		t.Errorf("degraded rate = %f, want > 0.10", rate)
	}
}
