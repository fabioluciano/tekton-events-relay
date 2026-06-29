package grafana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const testInstanceName = "grafana-prod"
const testGrafanaTemplate = "{{.State}}"

func TestNotifier_PostsAnnotation(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/annotations" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n, err := New(Config{
		Name:     testInstanceName,
		URL:      srv.URL,
		Token:    scm.NewStaticToken("sa-token"),
		Tags:     []string{"deploy"},
		Template: "{{.PipelineName}} {{.State}} ({{.RunName}})",
		Log:      zap.NewNop(),
	})
	if err != nil {
		t.Fatal(err)
	}

	err = n.Handle(context.Background(), domain.Event{
		PipelineName: "deploy-api", RunName: "run-1",
		State:      domain.StateSuccess,
		FinishedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if auth != "Bearer sa-token" {
		t.Errorf("auth = %q", auth)
	}
	if got["text"] != "deploy-api success (run-1)" {
		t.Errorf("text = %v", got["text"])
	}
	tags := got["tags"].([]any)
	if len(tags) != 3 || tags[0] != "tekton-events-relay" || tags[2] != "deploy" {
		t.Errorf("tags = %v", tags)
	}
	if int64(got["time"].(float64)) != time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("time = %v", got["time"])
	}
}

func postAndCapture(t *testing.T, cfg Config, e domain.Event) map[string]any {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg.URL = srv.URL
	if cfg.Token == nil {
		cfg.Token = scm.NewStaticToken("token")
	}
	if cfg.Log == nil {
		cfg.Log = zap.NewNop()
	}
	n, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return got
}

func TestNotifier_RegionAnnotation_TerminalWithStartAndFinish(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	finish := time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC)
	got := postAndCapture(t, Config{
		Name:     testInstanceName,
		Template: "{{.PipelineName}} {{.State}}",
	}, domain.Event{
		PipelineName: "deploy-api",
		State:        domain.StateSuccess,
		StartedAt:    start,
		FinishedAt:   finish,
	})

	if int64(got["time"].(float64)) != start.UnixMilli() {
		t.Errorf("time = %v, want start %d", got["time"], start.UnixMilli())
	}
	end, ok := got["timeEnd"]
	if !ok {
		t.Fatal("timeEnd missing for region annotation")
	}
	if int64(end.(float64)) != finish.UnixMilli() {
		t.Errorf("timeEnd = %v, want finish %d", end, finish.UnixMilli())
	}
}

func TestNotifier_RegionAnnotation_FinishOnlyUsesFinishForBoth(t *testing.T) {
	finish := time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC)
	got := postAndCapture(t, Config{
		Name:     testInstanceName,
		Template: testGrafanaTemplate,
	}, domain.Event{
		State:      domain.StateFailure,
		FinishedAt: finish,
	})

	if int64(got["time"].(float64)) != finish.UnixMilli() {
		t.Errorf("time = %v, want finish %d", got["time"], finish.UnixMilli())
	}
	if int64(got["timeEnd"].(float64)) != finish.UnixMilli() {
		t.Errorf("timeEnd = %v, want finish %d", got["timeEnd"], finish.UnixMilli())
	}
}

func TestNotifier_PointAnnotation_NonTerminalOmitsTimeEnd(t *testing.T) {
	got := postAndCapture(t, Config{
		Name:     testInstanceName,
		Template: testGrafanaTemplate,
	}, domain.Event{
		State: domain.StateRunning,
	})

	if _, ok := got["timeEnd"]; ok {
		t.Errorf("timeEnd present for non-terminal event: %v", got["timeEnd"])
	}
	if _, ok := got["time"]; !ok {
		t.Error("time missing for point annotation")
	}
}

func TestNotifier_ScopedAnnotation_IncludesDashboardAndPanel(t *testing.T) {
	got := postAndCapture(t, Config{
		Name:         testInstanceName,
		Template:     testGrafanaTemplate,
		DashboardUID: "jcIIG-07z",
		PanelID:      42,
	}, domain.Event{
		State:      domain.StateSuccess,
		FinishedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	})

	if got["dashboardUID"] != "jcIIG-07z" {
		t.Errorf("dashboardUID = %v", got["dashboardUID"])
	}
	if int(got["panelId"].(float64)) != 42 {
		t.Errorf("panelId = %v", got["panelId"])
	}
}

func TestNotifier_OrgAnnotation_OmitsDashboardAndPanelWhenUnset(t *testing.T) {
	got := postAndCapture(t, Config{
		Name:     testInstanceName,
		Template: testGrafanaTemplate,
	}, domain.Event{
		State:      domain.StateSuccess,
		FinishedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	})

	if _, ok := got["dashboardUID"]; ok {
		t.Errorf("dashboardUID present when unset: %v", got["dashboardUID"])
	}
	if _, ok := got["panelId"]; ok {
		t.Errorf("panelId present when unset: %v", got["panelId"])
	}
}

func TestNotifier_FileTemplate(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "grafana-test-*.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.Write([]byte("{{.PipelineName}} {{.State}}")); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n, err := New(Config{
		Name: "test", URL: srv.URL, Token: scm.NewStaticToken("token"),
		Template: tmpfile.Name(), Log: zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("New with file template: %v", err)
	}
	if n == nil {
		t.Error("expected notifier, got nil")
	}
}

func TestMultipleDashboards(t *testing.T) {
	var mu sync.Mutex
	var payloads []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		mu.Lock()
		payloads = append(payloads, got)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n, err := New(Config{
		Name:          testInstanceName,
		URL:           srv.URL,
		Token:         scm.NewStaticToken("token"),
		Template:      testGrafanaTemplate,
		DashboardUIDs: []string{"uid-1", "uid-2", "uid-3"},
		Log:           zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = n.Handle(context.Background(), domain.Event{
		State:      domain.StateSuccess,
		FinishedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 3 {
		t.Fatalf("expected 3 annotations, got %d", len(payloads))
	}

	seen := make(map[string]bool)
	for _, p := range payloads {
		uid, ok := p["dashboardUID"].(string)
		if !ok {
			t.Errorf("dashboardUID missing in payload: %v", p)
			continue
		}
		seen[uid] = true
	}
	for _, want := range []string{"uid-1", "uid-2", "uid-3"} {
		if !seen[want] {
			t.Errorf("missing annotation for dashboard %q", want)
		}
	}
}

func TestMultipleDashboards_SingularFallback(t *testing.T) {
	got := postAndCapture(t, Config{
		Name:         testInstanceName,
		Template:     testGrafanaTemplate,
		DashboardUID: "single-uid",
	}, domain.Event{
		State:      domain.StateSuccess,
		FinishedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	})

	if got["dashboardUID"] != "single-uid" {
		t.Errorf("dashboardUID = %v, want single-uid", got["dashboardUID"])
	}
}
