package http

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
)

type fakeSource struct{ names []string }

func (f fakeSource) Names() []string { return f.names }

func TestReadyz_JSONWithHandlerStatus(t *testing.T) {
	tracker := pipeline.NewStatusTracker()
	tracker.Observe("github", nil)
	tracker.Observe("github", errors.New("api 502"))
	tracker.Observe("slack", nil)

	health := buildHealthHandler(fakeSource{[]string{"github", "slack"}}, fakeSource{[]string{"taskrun"}}, tracker) //nolint:goconst // test fixture

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

func TestReadyz_UnavailableWithoutHandlers(t *testing.T) {
	health := buildHealthHandler(fakeSource{nil}, fakeSource{[]string{"taskrun"}}, nil)

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
