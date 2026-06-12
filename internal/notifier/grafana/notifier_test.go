package grafana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

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
		Name: "grafana-prod", URL: srv.URL, Token: "sa-token",
		Tags: []string{"deploy"}, Log: zap.NewNop(),
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
