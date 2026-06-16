package scm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStaticToken(t *testing.T) {
	st := NewStaticToken("my-token")
	tok, err := st.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "my-token" {
		t.Fatalf("got %q, want %q", tok, "my-token")
	}
}

func TestTokenTransport_Bearer(t *testing.T) {
	var capturedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := &TokenTransport{
		Base:      http.DefaultTransport,
		Refresher: NewStaticToken("test-token"),
		Style:     AuthStyleBearer,
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	want := "Bearer test-token"
	if capturedHeader != want {
		t.Fatalf("got Authorization %q, want %q", capturedHeader, want)
	}
}

func TestTokenTransport_CustomHeader(t *testing.T) {
	var capturedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("PRIVATE-TOKEN")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := &TokenTransport{
		Base:       http.DefaultTransport,
		Refresher:  NewStaticToken("gl-token"),
		Style:      AuthStyleHeader,
		HeaderName: "PRIVATE-TOKEN",
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if capturedHeader != "gl-token" {
		t.Fatalf("got PRIVATE-TOKEN %q, want %q", capturedHeader, "gl-token")
	}
}

func TestTokenTransport_RefreshError(t *testing.T) {
	transport := &TokenTransport{
		Base:      http.DefaultTransport,
		Refresher: &errorRefresher{err: errors.New("token expired")},
		Style:     AuthStyleBearer,
	}
	client := &http.Client{Transport: transport}

	_, err := client.Get("http://localhost:1") //nolint:bodyclose // error path
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTokenTransport_RefreshCalledPerRequest(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := &TokenTransport{
		Base: http.DefaultTransport,
		Refresher: &countingRefresher{
			token: "tok",
			count: &callCount,
		},
		Style: AuthStyleBearer,
	}
	client := &http.Client{Transport: transport}

	for i := 0; i < 3; i++ {
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		_ = resp.Body.Close()
	}

	if callCount != 3 {
		t.Fatalf("Token() called %d times, want 3", callCount)
	}
}

type errorRefresher struct {
	err error
}

func (e *errorRefresher) Token(_ context.Context) (string, error) {
	return "", e.err
}

type countingRefresher struct {
	token string
	count *int
}

func (c *countingRefresher) Token(_ context.Context) (string, error) {
	*c.count++
	return c.token, nil
}
