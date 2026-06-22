package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// rotatingRefresher returns a distinct token on every call, proving handlers
// resolve a fresh token per request instead of capturing one at build time.
type rotatingRefresher struct {
	mu    sync.Mutex
	calls int
}

func (r *rotatingRefresher) Token(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return fmt.Sprintf("rotated-token-%d", r.calls), nil
}

// authRecorder captures the Authorization header of every inbound request.
type authRecorder struct {
	mu   sync.Mutex
	seen []string
}

func (a *authRecorder) record(r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seen = append(a.seen, r.Header.Get("Authorization"))
}

func (a *authRecorder) len() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.seen)
}

func (a *authRecorder) at(i int) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.seen[i]
}

func prCommentTestEvent() domain.Event {
	n := 5
	return domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		RunName:  testRunName,
		State:    domain.StateSuccess,
		PRNumber: &n,
	}
}

func issueCommentTestEvent() domain.Event {
	n := 7
	return domain.Event{
		Provider:    providerGitHub,
		Repo:        domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		RunName:     testRunName,
		State:       domain.StateSuccess,
		IssueNumber: &n,
	}
}

func commitScopedTestEvent() domain.Event {
	return domain.Event{
		Provider:  providerGitHub,
		Repo:      domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		RunName:   testRunName,
		State:     domain.StateSuccess,
		CommitSHA: testHandlerSHA,
	}
}

func labelTestEvent() domain.Event {
	n := 9
	return domain.Event{
		Provider: providerGitHub,
		Repo:     domain.Repo{Owner: testHandlerOrg, Name: testHandlerRepo},
		State:    domain.StateSuccess,
		PRNumber: &n,
	}
}

// TestGitHubHandlers_TokenRefreshedPerRequest proves every handler resolves a
// fresh token through the live HTTPDoer on each Handle, never a static token
// captured at construction time.
func TestGitHubHandlers_TokenRefreshedPerRequest(t *testing.T) {
	tests := []struct {
		name       string
		newHandler func(HTTPDoer) (notifier.ActionHandler, error)
		event      domain.Event
	}{
		{
			name: "commit_comment",
			newHandler: func(c HTTPDoer) (notifier.ActionHandler, error) {
				return NewCommitCommentHandler(CommitCommentConfig{Client: c}, zap.NewNop())
			},
			event: commitScopedTestEvent(),
		},
		{
			name: "pr_comment",
			newHandler: func(c HTTPDoer) (notifier.ActionHandler, error) {
				return NewPRCommentHandler(PRCommentConfig{Client: c}, zap.NewNop())
			},
			event: prCommentTestEvent(),
		},
		{
			name: "issue_comment",
			newHandler: func(c HTTPDoer) (notifier.ActionHandler, error) {
				return NewIssueCommentHandler(IssueCommentConfig{Client: c}, zap.NewNop())
			},
			event: issueCommentTestEvent(),
		},
		{
			name: "check_run",
			newHandler: func(c HTTPDoer) (notifier.ActionHandler, error) {
				return NewCheckRunHandler(CheckRunConfig{Client: c}, zap.NewNop())
			},
			event: commitScopedTestEvent(),
		},
		{
			name: "label",
			newHandler: func(c HTTPDoer) (notifier.ActionHandler, error) {
				return NewLabelHandler(LabelConfig{
					Client: c,
					Labels: scm.LabelSet{Add: []scm.Label{{Name: "ci-passed"}}},
				}, zap.NewNop()), nil
			},
			event: labelTestEvent(),
		},
		{
			name: "deployment_status",
			newHandler: func(c HTTPDoer) (notifier.ActionHandler, error) {
				return NewDeploymentStatusHandler(DeploymentStatusConfig{Client: c}, zap.NewNop()), nil
			},
			event: commitScopedTestEvent(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: a server recording auth headers and a handler driven by a rotating refresher
			rec := &authRecorder{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.record(r)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				if strings.HasSuffix(r.URL.Path, "/labels") {
					_, _ = w.Write([]byte(`[{"id":1}]`))
					return
				}
				_, _ = w.Write([]byte(`{"id":1}`))
			}))
			defer server.Close()

			h, err := tt.newHandler(NewClientWithRefresher(&rotatingRefresher{}, server.URL, false, zap.NewNop(), false))
			if err != nil {
				t.Fatalf("build handler: %v", err)
			}

			// When: the handler runs twice
			startFirst := rec.len()
			if err := h.Handle(context.Background(), tt.event); err != nil {
				t.Fatalf("first Handle: %v", err)
			}
			endFirst := rec.len()
			if err := h.Handle(context.Background(), tt.event); err != nil {
				t.Fatalf("second Handle: %v", err)
			}
			endSecond := rec.len()

			// Then: both runs hit the API and the token changed between them
			if endFirst == startFirst {
				t.Fatal("first Handle made no API request")
			}
			if endSecond == endFirst {
				t.Fatal("second Handle made no API request")
			}
			firstToken := rec.at(endFirst - 1)
			secondToken := rec.at(endSecond - 1)
			if firstToken == secondToken {
				t.Fatalf("token did not refresh between calls: both %q", firstToken)
			}
			for i := 0; i < rec.len(); i++ {
				if !strings.HasPrefix(rec.at(i), "Bearer rotated-token-") {
					t.Fatalf("request %d carried unexpected auth %q", i, rec.at(i))
				}
			}
		})
	}
}
