package scm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// TestRenderTemplate_NilFallback documents the Category-2 (optional) SCM
// behavior: a nil template (empty config) renders the built-in
// "Build <State> for <RunName>" body instead of erroring.
func TestRenderTemplate_NilFallback(t *testing.T) {
	e := domain.Event{State: domain.StateSuccess, RunName: "build-1"}
	out, err := RenderTemplate(nil, e)
	if err != nil {
		t.Fatalf("RenderTemplate(nil) error: %v", err)
	}
	if out != "Build success for build-1" {
		t.Fatalf("fallback body = %q, want %q", out, "Build success for build-1")
	}
}

// TestLoadTemplateString_Inline confirms a non-path string is returned verbatim.
func TestLoadTemplateString_Inline(t *testing.T) {
	const inline = "Pipeline {{ .State }}"
	got, err := LoadTemplateString(inline)
	if err != nil {
		t.Fatalf("LoadTemplateString(inline) error: %v", err)
	}
	if got != inline {
		t.Fatalf("inline = %q, want %q", got, inline)
	}
}

// TestLoadTemplateString_FilePath confirms an absolute path is read from disk.
func TestLoadTemplateString_FilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.tmpl")
	if err := os.WriteFile(path, []byte("File {{ .State }}"), 0o600); err != nil {
		t.Fatalf("write temp template: %v", err)
	}
	got, err := LoadTemplateString(path)
	if err != nil {
		t.Fatalf("LoadTemplateString(path) error: %v", err)
	}
	if got != "File {{ .State }}" {
		t.Fatalf("file content = %q, want %q", got, "File {{ .State }}")
	}
}

// TestLoadTemplateString_MissingFile is the negative case: a configmapRef that
// resolves to an absolute /etc/templates path which does not exist must error,
// not silently fall back. This guards the regression that deleting a shipped
// template key (or mis-typing a configmapRef.key) would otherwise introduce.
func TestLoadTemplateString_MissingFile(t *testing.T) {
	_, err := LoadTemplateString("/etc/templates/tekton-events-relay-templates/does-not-exist.tmpl")
	if err == nil {
		t.Fatal("expected error for missing template file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read template file") {
		t.Fatalf("error = %v, want it to mention 'failed to read template file'", err)
	}
}

// TestCompileTemplate_MissingFile confirms the same missing-file failure
// propagates through CompileTemplate (the path SCM comment handlers use).
func TestCompileTemplate_MissingFile(t *testing.T) {
	_, err := CompileTemplate("pr_comment", "/etc/templates/tekton-events-relay-templates/does-not-exist.tmpl", nil)
	if err == nil {
		t.Fatal("expected error compiling a missing template file, got nil")
	}
}
