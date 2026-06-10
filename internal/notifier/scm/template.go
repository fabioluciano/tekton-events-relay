package scm

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// DefaultFuncMap returns the common template.FuncMap used by all SCM comment handlers.
// It starts from sprig.TxtFuncMap() to give templates access to upper/lower/trim/date/default/etc,
// then overlays domain-specific helpers which take precedence over any sprig equivalents.
func DefaultFuncMap() template.FuncMap {
	fm := sprig.TxtFuncMap()
	// Overlay domain-specific helpers — these take precedence over any sprig equivalents
	fm["IssueRef"] = FormatIssueRef
	fm["PRRef"] = FormatPRRef
	fm["UserMention"] = FormatUserMention
	fm["Truncate"] = Truncate // rune-aware with ellipsis — not covered by sprig
	return fm
}

// LoadTemplateString loads a template from an absolute file path.
// Template path must start with '/' (absolute path). Inline templates are no longer supported.
func LoadTemplateString(templateStr string) (string, error) {
	if !strings.HasPrefix(templateStr, "/") {
		return "", fmt.Errorf("template path must be absolute (start with /), got: %s", templateStr)
	}
	//nolint:gosec // G304: Template path from user configuration, validated by absolute path check
	data, err := os.ReadFile(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to read template file %s: %w", templateStr, err)
	}
	return string(data), nil
}

// CompileTemplate parses a named template with the given funcMap.
// Returns an error instead of silently producing a nil template.
// Pass nil for funcMap to use DefaultFuncMap.
func CompileTemplate(name, content string, funcMap template.FuncMap) (*template.Template, error) {
	text, err := LoadTemplateString(content)
	if err != nil {
		return nil, fmt.Errorf("load template %q: %w", name, err)
	}
	if funcMap == nil {
		funcMap = DefaultFuncMap()
	}
	return template.New(name).Funcs(funcMap).Parse(text)
}

// RenderTemplate executes tmpl against the event, or returns a default message if tmpl is nil.
func RenderTemplate(tmpl *template.Template, e domain.Event) (string, error) {
	if tmpl == nil {
		return fmt.Sprintf("Build %s for %s", e.State, e.RunName), nil
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, e); err != nil {
		return "", err
	}
	return buf.String(), nil
}
