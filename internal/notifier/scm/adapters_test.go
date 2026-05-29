package scm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/azuredevops"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/bitbucket"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/gitea"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/github"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm/gitlab"
)

const (
	testTokenValue   = "tk"
	testCommitSHAAbc = "abc"
	testStateField   = "state"
	testStateSuccess = "success"
	testOwnerFabio   = "fabioluciano"
	testRepoPipeline = "tekton-events-relay"
	testAuthField    = "Authorization"
	testTokenPrefix  = "token tk"
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
	r := github.NewStatusReporter(testTokenValue, srv.URL, false)
	err := r.Handle(context.Background(), domain.Event{
		Provider: "github",
		Repo:      domain.Repo{Owner: testOwnerFabio, Name: testRepoPipeline},
		CommitSHA: "abc123", State: domain.StateSuccess,
		Context: "tekton/build",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.path, "/repos/fabioluciano/tekton-events-relay/statuses/abc123") {
		t.Errorf("path = %q", c.path)
	}
	if c.body[testStateField] != testStateSuccess {
		t.Errorf("state = %v", c.body[testStateField])
	}
}

func TestGitLab_Notify(t *testing.T) {
	srv, c := capture(t, 201)
	r := gitlab.NewServer(gitlab.Config{Token: testTokenValue, BaseURL: srv.URL})
	err := r.Notify(context.Background(), domain.Event{
		Repo: domain.Repo{ID: "42"}, CommitSHA: testCommitSHAAbc,
		State: domain.StateFailure,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.path, "/projects/42/statuses/abc") {
		t.Errorf("path = %q", c.path)
	}
	if c.body[testStateField] != "failed" {
		t.Errorf("state = %v", c.body[testStateField])
	}
}

func TestBitbucketCloud_Notify(t *testing.T) {
	srv, c := capture(t, 201)
	r := bitbucket.NewCloud(bitbucket.CloudConfig{Username: "u", AppPassword: "p", BaseURL: srv.URL})
	err := r.Notify(context.Background(), domain.Event{
		Repo:      domain.Repo{Workspace: testOwnerFabio, Name: "repo"},
		CommitSHA: testCommitSHAAbc, State: domain.StateRunning, Context: "ci",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.body[testStateField] != "INPROGRESS" {
		t.Errorf("state = %v", c.body[testStateField])
	}
}

func TestBitbucketServer_Notify(t *testing.T) {
	srv, c := capture(t, 204)
	r := bitbucket.NewServer(bitbucket.ServerConfig{Token: testTokenValue, BaseURL: srv.URL})
	err := r.Notify(context.Background(), domain.Event{
		CommitSHA: testCommitSHAAbc, State: domain.StateSuccess, Context: "ci",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.body[testStateField] != "SUCCESSFUL" {
		t.Errorf("state = %v", c.body[testStateField])
	}
}

func TestAzureDevOps_Notify(t *testing.T) {
	srv, c := capture(t, 200)
	r := azuredevops.New(azuredevops.Config{Token: testTokenValue, BaseURL: srv.URL, Genre: "tekton-ci"})
	err := r.Notify(context.Background(), domain.Event{
		Repo:      domain.Repo{Org: "myorg", Project: "myproj", Name: "myrepo"},
		CommitSHA: testCommitSHAAbc, State: domain.StateSuccess, Context: "build",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, ok := c.body["context"].(map[string]any)
	if !ok {
		t.Fatalf("context not nested: %T", c.body["context"])
	}
	if ctx["genre"] != "tekton-ci" {
		t.Errorf("genre = %v", ctx["genre"])
	}
}

func TestGitea_Notify(t *testing.T) {
	srv, c := capture(t, 201)
	r := gitea.New(gitea.Config{Token: testTokenValue, BaseURL: srv.URL})
	err := r.Notify(context.Background(), domain.Event{
		Repo:      domain.Repo{Owner: testOwnerFabio, Name: "repo"},
		CommitSHA: testCommitSHAAbc, State: domain.StateSuccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.auth != testTokenPrefix {
		t.Errorf("auth = %q", c.auth)
	}
}
