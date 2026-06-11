package github

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
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

func TestLabelHandler_LabelSetAddRemove(t *testing.T) {
	var mu sync.Mutex
	var added []string
	var removed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodDelete:
			parts := strings.Split(r.URL.Path, "/")
			name := parts[len(parts)-1]
			if name == "ci::missing" {
				w.WriteHeader(http.StatusNotFound) // absent label: must be tolerated
				return
			}
			removed = append(removed, name)
			w.WriteHeader(http.StatusOK)
		case http.MethodPost:
			var p map[string][]string
			_ = json.NewDecoder(r.Body).Decode(&p)
			added = append(added, p["labels"]...)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	h := NewLabelHandler(LabelConfig{
		Token:   testHandlerToken,
		BaseURL: srv.URL,
		Labels: scm.LabelSet{
			Add:    []string{"ci::{{.State}}"},
			Remove: []string{"ci::running", "ci::missing"},
		},
	}, zap.NewNop())

	pr := 5
	err := h.Handle(context.Background(), domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		PRNumber: &pr,
		State:    domain.StateSuccess,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(added) != 1 || added[0] != "ci::success" {
		t.Errorf("added = %v, want [ci::success] (templated)", added)
	}
	if len(removed) != 1 || removed[0] != "ci::running" {
		t.Errorf("removed = %v, want [ci::running] (404 tolerated)", removed)
	}
}

func TestLabelHandler_LegacyPathPreserved(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := NewLabelHandler(LabelConfig{
		Token: testHandlerToken, BaseURL: srv.URL,
		SuccessLabel: "ok", FailureLabel: "bad",
	}, zap.NewNop())

	pr := 5
	e := domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		PRNumber: &pr, State: domain.StateSuccess,
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if posts != 1 {
		t.Errorf("posts = %d, want 1 (legacy behavior)", posts)
	}
}
