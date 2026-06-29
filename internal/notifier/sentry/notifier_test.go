package sentry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	testSentryOrg     = "acme"
	testSentryProject = "api"
)

func TestNotifier_CreatesReleaseAndDeploy(t *testing.T) {
	var paths []string
	var deployBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.Contains(r.URL.Path, "/deploys/") {
			_ = json.NewDecoder(r.Body).Decode(&deployBody)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	n := New(Config{Name: "sentry-prod", BaseURL: srv.URL, Token: scm.NewStaticToken("t"), Org: testSentryOrg,
		Projects: []string{testSentryProject}, Log: zap.NewNop()})

	e := domain.Event{
		CommitSHA: "abc123", RunName: "run-1", Context: "production",
		Repo: domain.Repo{Owner: testSentryOrg, Name: testSentryProject}, State: domain.StateSuccess,
	}
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(paths) != 2 ||
		!strings.Contains(paths[0], "/organizations/acme/releases/") ||
		!strings.Contains(paths[1], "/releases/abc123/deploys/") {
		t.Errorf("paths = %v", paths)
	}
	if deployBody["environment"] != "production" {
		t.Errorf("deploy env = %v", deployBody["environment"])
	}

	// Non-success and missing SHA skip silently.
	e.State = domain.StateFailure
	_ = n.Handle(context.Background(), e)
	e.State = domain.StateSuccess
	e.CommitSHA = ""
	_ = n.Handle(context.Background(), e)
	if len(paths) != 2 {
		t.Errorf("paths = %d, want 2 (skips)", len(paths))
	}
}

func TestEnvironmentExpr(t *testing.T) {
	envExpr, err := cel.CompileString(`event.Namespace == "staging" ? "staging" : "production"`)
	if err != nil {
		t.Fatalf("compile environment_expr: %v", err)
	}

	var deployBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/deploys/") {
			_ = json.NewDecoder(r.Body).Decode(&deployBody)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	n := New(Config{
		Name:            "sentry-prod",
		BaseURL:         srv.URL,
		Token:           scm.NewStaticToken("t"),
		Org:             testSentryOrg,
		Projects:        []string{testSentryProject},
		EnvironmentExpr: envExpr,
		Log:             zap.NewNop(),
	})

	// staging namespace → "staging" from CEL
	e := domain.Event{
		CommitSHA: "abc123", RunName: "run-1", Namespace: "staging",
		Repo: domain.Repo{Owner: testSentryOrg, Name: testSentryProject}, State: domain.StateSuccess,
	}
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if deployBody["environment"] != "staging" {
		t.Errorf("deploy env = %v, want staging", deployBody["environment"])
	}

	// default namespace → "production" from CEL
	e.Namespace = "default"
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if deployBody["environment"] != "production" {
		t.Errorf("deploy env = %v, want production", deployBody["environment"])
	}
}

func TestEnvironmentExpr_EmptyFallsBackToContext(t *testing.T) {
	var deployBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/deploys/") {
			_ = json.NewDecoder(r.Body).Decode(&deployBody)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	// No EnvironmentExpr — falls back to e.Context
	n := New(Config{
		Name:     "sentry-prod",
		BaseURL:  srv.URL,
		Token:    scm.NewStaticToken("t"),
		Org:      testSentryOrg,
		Projects: []string{testSentryProject},
		Log:      zap.NewNop(),
	})

	e := domain.Event{
		CommitSHA: "abc123", RunName: "run-1", Context: "staging",
		Repo: domain.Repo{Owner: testSentryOrg, Name: testSentryProject}, State: domain.StateSuccess,
	}
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if deployBody["environment"] != "staging" {
		t.Errorf("deploy env = %v, want staging", deployBody["environment"])
	}
}
