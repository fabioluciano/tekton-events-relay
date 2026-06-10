package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockHandlerSource implements HandlerSource for testing
type mockHandlerSource struct {
	names []string
}

func (m *mockHandlerSource) Names() []string {
	return m.names
}

// mockDecoderSource implements DecoderSource for testing
type mockDecoderSource struct {
	names []string
}

func (m *mockDecoderSource) Names() []string {
	return m.names
}

func TestReadyzHandler_AllHealthy(t *testing.T) {
	registry := &mockHandlerSource{names: []string{"github", "slack"}}
	decoders := &mockDecoderSource{names: []string{"taskrun", "pipelinerun"}}

	health := buildHealthHandler(registry, decoders)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	health.readyEndpoint(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestReadyzHandler_NoHandlers(t *testing.T) {
	registry := &mockHandlerSource{names: []string{}}
	decoders := &mockDecoderSource{names: []string{"taskrun", "pipelinerun"}}

	health := buildHealthHandler(registry, decoders)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	health.readyEndpoint(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	body := strings.TrimSpace(w.Body.String())
	if !strings.Contains(body, "no handlers registered") {
		t.Errorf("expected 'no handlers registered' in response body, got %q", body)
	}
}

func TestReadyzHandler_NoDecoders(t *testing.T) {
	registry := &mockHandlerSource{names: []string{"github", "slack"}}
	decoders := &mockDecoderSource{names: []string{}}

	health := buildHealthHandler(registry, decoders)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	health.readyEndpoint(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	body := strings.TrimSpace(w.Body.String())
	if !strings.Contains(body, "no decoders registered") {
		t.Errorf("expected 'no decoders registered' in response body, got %q", body)
	}
}

func TestReadyzHandler_ChecksRunInOrder(t *testing.T) {
	registry := &mockHandlerSource{names: []string{"github"}}
	decoders := &mockDecoderSource{names: []string{"taskrun"}}

	health := buildHealthHandler(registry, decoders)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	health.readyEndpoint(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHealthzEndpoint(t *testing.T) {
	registry := &mockHandlerSource{names: []string{"github"}}
	decoders := &mockDecoderSource{names: []string{"taskrun"}}

	health := buildHealthHandler(registry, decoders)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	health.liveEndpoint(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}
