package scm

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// LabelSet declares the label effect of a label action: which labels to add
// and which to remove. The action's `when` CEL expression is the only
// execution gate; entries support Go templates (e.g. "ci::{{.State}}").
// Removal runs before addition so overlapping names converge.
type LabelSet struct {
	Add    []string
	Remove []string
}

// Empty reports whether no add/remove effect is declared (legacy
// success_label/failure_label behavior applies).
func (s LabelSet) Empty() bool { return len(s.Add) == 0 && len(s.Remove) == 0 }

// Render evaluates the template entries against the event, returning the
// concrete add and remove label lists. Empty results are dropped.
func (s LabelSet) Render(e domain.Event) (add, remove []string, err error) {
	add, err = renderLabelList(s.Add, e)
	if err != nil {
		return nil, nil, fmt.Errorf("render add labels: %w", err)
	}
	remove, err = renderLabelList(s.Remove, e)
	if err != nil {
		return nil, nil, fmt.Errorf("render remove labels: %w", err)
	}
	return add, remove, nil
}

func renderLabelList(items []string, e domain.Event) ([]string, error) {
	out := make([]string, 0, len(items))
	for _, raw := range items {
		tmpl, err := template.New("label").Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse label template %q: %w", raw, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, e); err != nil {
			return nil, fmt.Errorf("execute label template %q: %w", raw, err)
		}
		if v := buf.String(); v != "" {
			out = append(out, v)
		}
	}
	return out, nil
}
