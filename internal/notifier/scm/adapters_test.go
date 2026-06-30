package scm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/github"
)

const (
	testTokenValue   = "tk"
	testCommitSHA123 = "abc123"
	testStateField   = "state"
	testStateSuccess = "success"
	testOwnerFabio   = "fabioluciano"
	testRepoPipeline = "tekton-events-relay"
	testContextBuild = "tekton/build"
	testRepoPath     = "/repos/fabioluciano/tekton-events-relay/statuses/"
)

type captured struct {
	method string
	path   string
	auth   string
	body   map[string]any
}

func capture(t *testing.T, status int) (*httptest.Server, *captured) {
	t.Helper()
	c := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.method = r.Method
		c.path = r.URL.Path + "?" + r.URL.RawQuery
		c.auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &c.body)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func TestGitHub_Notify(t *testing.T) {
	srv, c := capture(t, 201)
	client := github.NewClient(testTokenValue, srv.URL, false, zap.NewNop(), false)
	r := github.NewStatusReporter(client, "github", zap.NewNop())
	err := r.Handle(context.Background(), domain.Event{
		Provider:  "github",
		Repo:      domain.Repo{Owner: testOwnerFabio, Name: testRepoPipeline},
		CommitSHA: testCommitSHA123, State: domain.StateSuccess,
		Context: testContextBuild,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.path, testRepoPath+testCommitSHA123) {
		t.Errorf("path = %q", c.path)
	}
	if c.body[testStateField] != testStateSuccess {
		t.Errorf("state = %v", c.body[testStateField])
	}
}
