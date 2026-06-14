package scm

import (
	"bytes"
	"fmt"
	"regexp"
	"sync"
	"text/template"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

var (
	colorRegex  = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)
	tmplCacheMu sync.Mutex
	tmplCache   = make(map[string]*template.Template)
)

// Label represents a label with an optional color.
type Label struct {
	Name  string `json:"name" yaml:"name"`
	Color string `json:"color,omitempty" yaml:"color,omitempty"` // hex without #, e.g. "d73a4a"
}

// LabelSet declares the label effect of a label action: which labels to add
// and which to remove. The action's `when` CEL expression is the only
// execution gate; entries support Go templates (e.g. "ci::{{.State}}").
// Removal runs before addition so overlapping names converge.
type LabelSet struct {
	Add    []Label `json:"add" yaml:"add"`
	Remove []Label `json:"remove" yaml:"remove"`
}

// Empty reports whether no add/remove effect is declared (the action
// is a no-op).
func (s LabelSet) Empty() bool { return len(s.Add) == 0 && len(s.Remove) == 0 }

// Validate checks that all label colors are valid hex strings.
// Invalid colors are logged as warnings and replaced with empty strings.
func (s *LabelSet) Validate(log *zap.Logger) {
	for i := range s.Add {
		if s.Add[i].Color != "" && !colorRegex.MatchString(s.Add[i].Color) {
			if log != nil {
				log.Warn("invalid label color, using provider default",
					zap.String("label", s.Add[i].Name),
					zap.String("invalid_color", s.Add[i].Color))
			}
			s.Add[i].Color = ""
		}
	}
	for i := range s.Remove {
		if s.Remove[i].Color != "" && !colorRegex.MatchString(s.Remove[i].Color) {
			if log != nil {
				log.Warn("invalid label color (ignored for removal)",
					zap.String("label", s.Remove[i].Name),
					zap.String("invalid_color", s.Remove[i].Color))
			}
			s.Remove[i].Color = ""
		}
	}
}

// Render evaluates the template entries against the event, returning the
// concrete add and remove label lists. Empty results are dropped.
func (s LabelSet) Render(e domain.Event) (add, remove []Label, err error) {
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

func cachedTemplate(name string) (*template.Template, error) {
	tmplCacheMu.Lock()
	defer tmplCacheMu.Unlock()
	if t, ok := tmplCache[name]; ok {
		return t, nil
	}
	t, err := template.New("label").Parse(name)
	if err != nil {
		return nil, err
	}
	tmplCache[name] = t
	return t, nil
}

func renderLabelList(items []Label, e domain.Event) ([]Label, error) {
	out := make([]Label, 0, len(items))
	for _, label := range items {
		tmpl, err := cachedTemplate(label.Name)
		if err != nil {
			return nil, fmt.Errorf("parse label template %q: %w", label.Name, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, e); err != nil {
			return nil, fmt.Errorf("execute label template %q: %w", label.Name, err)
		}
		if v := buf.String(); v != "" {
			out = append(out, Label{Name: v, Color: label.Color})
		}
	}
	return out, nil
}
