package github

import (
	"testing"

	"go.uber.org/zap"
)

// TestNewPRCommentHandler_EmptyTemplateOK proves the Category-2 (optional)
// contract: an SCM comment handler constructs successfully with an empty
// template. At runtime the nil template falls back to scm.RenderTemplate's
// "Build <State> for <RunName>" body — empty is NOT an error.
func TestNewPRCommentHandler_EmptyTemplateOK(t *testing.T) {
	h, err := NewPRCommentHandler(PRCommentConfig{
		Token:   "t",
		BaseURL: testAPIURL,
		// Template intentionally omitted
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("empty template should construct without error, got: %v", err)
	}
	if h == nil {
		t.Fatal("expected handler, got nil")
	}
}

// TestNewPRCommentHandler_InlineTemplateOK confirms the inline form compiles.
func TestNewPRCommentHandler_InlineTemplateOK(t *testing.T) {
	h, err := NewPRCommentHandler(PRCommentConfig{
		Token:    "t",
		BaseURL:  testAPIURL,
		Template: "Pipeline {{ .State }}: {{ .RunName }}",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("inline template should construct, got: %v", err)
	}
	if h == nil {
		t.Fatal("expected handler, got nil")
	}
}

// TestNewPRCommentHandler_InvalidTemplateRejected confirms a malformed inline
// template fails fast at construction.
func TestNewPRCommentHandler_InvalidTemplateRejected(t *testing.T) {
	if _, err := NewPRCommentHandler(PRCommentConfig{
		Token:    "t",
		BaseURL:  testAPIURL,
		Template: "{{ .Oops",
	}, zap.NewNop()); err == nil {
		t.Fatal("expected error for malformed template, got nil")
	}
}
