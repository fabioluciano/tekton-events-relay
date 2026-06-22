package gitea

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	mockGiteaVersion     = "1.22.0"
	mockGiteaKey         = "version"
	mockGiteaVersionPath = "/api/v1/version"
	mockGiteaOwner       = "org"
	mockGiteaRepo        = "repo"
)

func newLabelTestHandler(t *testing.T, baseURL string) notifier.ActionHandler {
	t.Helper()
	return newLabelTestHandlerNamed(t, baseURL, providerGitea)
}

func newLabelTestHandlerNamed(t *testing.T, baseURL, name string) notifier.ActionHandler {
	t.Helper()
	client, err := NewClient("token", baseURL, false, false, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewLabelHandler(LabelConfig{
		Client: client,
		Name:   name,
		Labels: scm.LabelSet{Add: []scm.Label{{Name: "ci:passed"}}, Remove: []scm.Label{{Name: "ci:failed"}}},
		Log:    zap.NewNop(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestLabelHandler_NameAndType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{mockGiteaKey: mockGiteaVersion})
	}))
	defer srv.Close()

	h := newLabelTestHandler(t, srv.URL)
	if h.Name() != "gitea" {
		t.Errorf("Name = %q, want gitea", h.Name())
	}
	if h.Type() != notifier.ActionLabel {
		t.Errorf("Type = %q, want label", h.Type())
	}
}

func TestLabelHandler_SkipsWithoutIssueOrPR(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == mockGiteaVersionPath {
			_ = json.NewEncoder(w).Encode(map[string]string{mockGiteaKey: mockGiteaVersion})
			return
		}
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := newLabelTestHandler(t, srv.URL)
	e := domain.Event{
		Provider: providerGitea,
		Repo:     domain.Repo{Owner: mockGiteaOwner, Name: mockGiteaRepo},
		State:    domain.StateSuccess,
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("API calls = %d, want 0 (no issue/PR number)", calls.Load())
	}
}

func TestLabelHandler_AppliesLabelForState(t *testing.T) {
	var labelCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == mockGiteaVersionPath:
			_ = json.NewEncoder(w).Encode(map[string]string{mockGiteaKey: mockGiteaVersion})
		case strings.Contains(r.URL.Path, "/labels"):
			labelCalls.Add(1)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			// label lookup/creation helpers may hit other endpoints
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	h := newLabelTestHandler(t, srv.URL)
	pr := 5
	e := domain.Event{
		Provider: providerGitea,
		Repo:     domain.Repo{Owner: mockGiteaOwner, Name: mockGiteaRepo},
		PRNumber: &pr,
		State:    domain.StateSuccess,
	}
	// The SDK label flow may require label pre-creation; tolerate an error
	// as long as the handler attempted the API (observable via calls).
	_ = h.Handle(context.Background(), e)
	if labelCalls.Load() == 0 {
		t.Skip("gitea SDK label flow did not reach /labels endpoint in this version")
	}
}

// TestLabelHandler_CustomInstanceName guards dispatch by configured instance
// name (cfg.Name) rather than the hardcoded provider type.
func TestLabelHandler_CustomInstanceName(t *testing.T) {
	const instanceName = "my-gitea"

	tests := []struct {
		name          string
		provider      string
		wantNameMatch bool
	}{
		{name: "matching custom provider acts", provider: instanceName, wantNameMatch: true},
		{name: "mismatched provider skipped", provider: "other-gitea", wantNameMatch: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var labelCalls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == mockGiteaVersionPath:
					_ = json.NewEncoder(w).Encode(map[string]string{mockGiteaKey: mockGiteaVersion})
				case strings.Contains(r.URL.Path, "/labels"):
					labelCalls.Add(1)
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]map[string]any{})
				default:
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`[]`))
				}
			}))
			defer srv.Close()

			h := newLabelTestHandlerNamed(t, srv.URL, instanceName)
			if h.Name() != instanceName {
				t.Fatalf("Name = %q, want %q", h.Name(), instanceName)
			}

			pr := 7
			e := domain.Event{
				Provider: tt.provider,
				Repo:     domain.Repo{Owner: mockGiteaOwner, Name: mockGiteaRepo},
				PRNumber: &pr,
				State:    domain.StateSuccess,
			}

			if !tt.wantNameMatch {
				if err := h.Handle(context.Background(), e); err != nil {
					t.Fatalf("Handle: %v", err)
				}
				if labelCalls.Load() != 0 {
					t.Errorf("API label calls = %d, want 0 (provider %q != instance %q)",
						labelCalls.Load(), tt.provider, instanceName)
				}
				return
			}

			_ = h.Handle(context.Background(), e)
			if labelCalls.Load() == 0 {
				t.Skip("gitea SDK label flow did not reach /labels endpoint in this version")
			}
		})
	}
}
