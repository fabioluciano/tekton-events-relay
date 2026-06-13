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

// dangerousFuncs lists sprig functions removed from the safe template map
// to prevent environment variable exfiltration and cryptographic misuse.
var dangerousFuncs = map[string]bool{
	"env":               true,
	"expandenv":         true,
	"expand":            true,
	"base64Encode":      true,
	"base64Decode":      true,
	"genPrivateKey":     true,
	"genCA":             true,
	"genSelfSignedCert": true,
}

// safeFuncMap returns a copy of sprig.TxtFuncMap() with dangerous functions removed.
func safeFuncMap() template.FuncMap {
	fm := sprig.TxtFuncMap()
	for name := range dangerousFuncs {
		delete(fm, name)
	}
	return fm
}

// DefaultFuncMap returns the common template.FuncMap used by all SCM comment handlers.
// It starts from a safe subset of sprig.TxtFuncMap() to give templates access to
// upper/lower/trim/date/default/etc, then overlays domain-specific helpers which
// take precedence over any sprig equivalents.
func DefaultFuncMap() template.FuncMap {
	fm := safeFuncMap()
	// Overlay domain-specific helpers — these take precedence over any sprig equivalents
	fm["IssueRef"] = FormatIssueRef
	fm["PRRef"] = FormatPRRef
	fm["UserMention"] = FormatUserMention
	fm["Truncate"] = Truncate // rune-aware with ellipsis — not covered by sprig
	return fm
}

// LoadTemplateString loads a template from an absolute file path or returns inline content.
// If templateStr starts with '/', it's treated as an absolute file path.
// Otherwise, it's returned as-is (inline template).
func LoadTemplateString(templateStr string) (string, error) {
	if strings.HasPrefix(templateStr, "/") {
		//nolint:gosec // G304: Template path from user configuration, validated by absolute path check
		data, err := os.ReadFile(templateStr)
		if err != nil {
			return "", fmt.Errorf("failed to read template file %s: %w", templateStr, err)
		}
		return string(data), nil
	}
	// Inline template - return as-is
	return templateStr, nil
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
