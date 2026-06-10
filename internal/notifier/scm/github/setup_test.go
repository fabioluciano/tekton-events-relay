package github

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	// Setup: create template files used by tests
	templateDir := "/tmp/tekton-test-templates"
	if err := os.MkdirAll(templateDir, 0750); err != nil {
		panic(err)
	}

	templates := map[string]string{
		"checkrun.tmpl":   "Run: {{.RunName}} | State: {{.State}}",
		"discussion.tmpl": "Discussion: {{.State}}",
		"issue.tmpl":      "Issue: {{.State}}",
		"pr.tmpl":         "PR: {{.State}}",
		"msg.tmpl":        "Message: {{.State}}",
	}

	for name, content := range templates {
		path := filepath.Join(templateDir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			panic(err)
		}
	}

	// Run tests
	code := m.Run()

	// Cleanup
	_ = os.RemoveAll(templateDir)

	os.Exit(code)
}
