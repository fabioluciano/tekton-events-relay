// Package grafana posts deployment markers to the Grafana Annotations API,
// so pipeline events appear as vertical markers on dashboards.
package grafana

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"text/template"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const defaultText = "{{.PipelineName}} {{.State}} ({{.RunName}})"

// Notifier posts annotations to Grafana.
type Notifier struct {
	base     *notifier.Base
	name     string
	tags     []string
	template *template.Template
}

// Config configures the Grafana notifier.
type Config struct {
	Name     string
	URL      string // Grafana base URL (e.g. https://grafana.example.com)
	Token    string // service account token
	Tags     []string
	Template string
	Log      *zap.Logger
}

// New creates a Grafana annotations notifier.
func New(cfg Config) (*Notifier, error) {
	tmplSrc := cfg.Template
	if tmplSrc == "" {
		tmplSrc = defaultText
	}
	tmpl, err := scm.CompileTemplate("grafana", tmplSrc, nil)
	if err != nil {
		return nil, fmt.Errorf("compile template: %w", err)
	}

	n := &Notifier{name: cfg.Name, tags: cfg.Tags, template: tmpl}
	url := strings.TrimRight(cfg.URL, "/") + "/api/annotations"
	token := cfg.Token

	n.base = &notifier.Base{
		HTTP:      notifier.DefaultHTTPClient(),
		UserAgent: notifier.UserAgent,
		Log:       cfg.Log,
		BuildURL:  func(domain.Event) (string, error) { return url, nil },
		Auth: func(req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		},
		BuildPayload: func(e domain.Event) (any, error) {
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, e); err != nil {
				return nil, fmt.Errorf("execute template: %w", err)
			}
			tags := append([]string{"tekton-events-relay", string(e.State)}, n.tags...)
			return map[string]any{
				"time": annotationTime(e),
				"text": buf.String(),
				"tags": tags,
			}, nil
		},
	}
	return n, nil
}

// annotationTime uses the run finish time when available (epoch millis).
func annotationTime(e domain.Event) int64 {
	t := e.FinishedAt
	if t.IsZero() {
		t = time.Now()
	}
	return t.UnixMilli()
}

// Name returns the notifier name.
func (n *Notifier) Name() string { return n.name }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle posts the annotation.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	return n.base.Send(ctx, e)
}
