package webhook

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
)

// rotatingRefresher returns a different token on every call, simulating an
// OAuth2 refresh or a re-read of a rotated mounted secret.
type rotatingRefresher struct{ n int }

func (r *rotatingRefresher) Token(_ context.Context) (string, error) {
	r.n++
	return "token-" + strconv.Itoa(r.n), nil
}

type errRefresher struct{}

func (errRefresher) Token(_ context.Context) (string, error) {
	return "", errors.New("refresh failed")
}

func TestApplyAuth_OAuth2SetsBearer(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://example.com", nil)
	auth := &ResolvedAuth{Type: authTypeOAuth2, Token: &rotatingRefresher{}}

	if err := applyAuth(req, auth); err != nil {
		t.Fatalf("applyAuth: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer token-1" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer token-1")
	}
}

func TestApplyAuth_BearerReResolvesPerRequest(t *testing.T) {
	refresher := &rotatingRefresher{}
	auth := &ResolvedAuth{Type: authTypeBearer, Token: refresher}

	for i := 1; i <= 3; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://example.com", nil)
		if err := applyAuth(req, auth); err != nil {
			t.Fatalf("applyAuth #%d: %v", i, err)
		}
		want := "Bearer token-" + strconv.Itoa(i)
		if got := req.Header.Get("Authorization"); got != want {
			t.Fatalf("request #%d Authorization = %q, want %q", i, got, want)
		}
	}
}

func TestApplyAuth_RefreshErrorPropagates(t *testing.T) {
	cases := map[string]*ResolvedAuth{
		"bearer": {Type: authTypeBearer, Token: errRefresher{}},
		"apikey": {Type: authTypeAPIKey, Token: errRefresher{}, Header: "X-API-Key"},
		"basic":  {Type: authTypeBasic, Username: "u", Password: errRefresher{}},
	}
	for name, auth := range cases {
		t.Run(name, func(t *testing.T) {
			req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://example.com", nil)
			if err := applyAuth(req, auth); err == nil {
				t.Fatal("expected error from failed token refresh, got nil")
			}
		})
	}
}
