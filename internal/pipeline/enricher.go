package pipeline

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// Enricher fills derived fields that don't come from the CloudEvent.
type Enricher struct {
	BaseHandler
	DashboardURL string // ex: https://tekton.company.com
}

// NewEnricher creates a new Enricher with the given Tekton dashboard URL.
func NewEnricher(dashboard string) *Enricher {
	return &Enricher{DashboardURL: strings.TrimRight(dashboard, "/")}
}

// Handle enriches the event with derived fields such as the dashboard link.
// If DashboardURL is set and TargetURL is empty, it generates a dashboard link.
func (e *Enricher) Handle(ctx context.Context, env *event.Envelope) error {
	if e.DashboardURL != "" && env.Report.TargetURL == "" {
		env.Report.TargetURL = e.dashboardLink(env.Report)
	}
	return e.Next(ctx, env)
}

func (e *Enricher) dashboardLink(r domain.Event) string {
	var kind string
	switch r.Resource {
	case domain.ResourceTaskRun:
		kind = "taskruns"
	case domain.ResourcePipelineRun:
		kind = "pipelineruns"
	case domain.ResourceCustomRun:
		kind = "customruns"
	default:
		// EventListener and unknown resources have no dashboard page.
		return ""
	}
	return fmt.Sprintf("%s/#/namespaces/%s/%s/%s",
		e.DashboardURL, url.PathEscape(r.Namespace), kind, url.PathEscape(r.RunName))
}
