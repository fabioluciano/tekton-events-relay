package accumulator

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"text/template"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const notAvailable = "N/A"

// Handler accumulates TaskRun events and posts aggregate PR comments
// when a PipelineRun reaches a terminal state.
type Handler struct {
	name     string
	buffer   Buffer
	provider notifier.ActionHandler
	tmpl     *template.Template
	log      *zap.Logger
}

// NewHandler creates an accumulator that wraps a PR comment handler.
func NewHandler(name string, provider notifier.ActionHandler, buf Buffer, log *zap.Logger) *Handler {
	return &Handler{
		name:     name,
		buffer:   buf,
		provider: provider,
		log:      log,
	}
}

// SetTemplate configures a custom Go text/template for PR summary rendering.
// The template receives a *SummaryData value; nil reverts to the default markdown table.
func (a *Handler) SetTemplate(tmpl *template.Template) {
	a.tmpl = tmpl
}

// Name returns the instance name.
func (a *Handler) Name() string { return a.name }

// Provider returns the provider type identifier.
func (a *Handler) Provider() string { return "accumulator" }

// Type returns the action type.
func (a *Handler) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle processes events:
//   - TaskRun: accumulate in buffer (using group key if present)
//   - PipelineRun non-terminal: accumulate in buffer
//   - PipelineRun terminal: check group completion or flush single run
//
// Other events are ignored.
func (a *Handler) Handle(ctx context.Context, event domain.Event) error {
	uid := event.RunID
	if uid == "" {
		return nil
	}

	groupID := event.AccumulatorGroupID

	if groupID != "" {
		return a.handleGrouped(ctx, event, groupID, uid)
	}
	return a.handleSingle(ctx, event, uid)
}

func (a *Handler) handleSingle(ctx context.Context, event domain.Event, uid string) error {
	if event.Resource == domain.ResourceTaskRun || event.Resource == domain.ResourcePipelineRun {
		a.buffer.Add(ctx, uid, &event)
	}

	if event.Resource == domain.ResourcePipelineRun && isTerminalState(event.State) {
		return a.flushAndPost(ctx, uid, event)
	}

	return nil
}

func (a *Handler) handleGrouped(ctx context.Context, event domain.Event, groupID, uid string) error {
	if event.Resource == domain.ResourceTaskRun || event.Resource == domain.ResourcePipelineRun {
		a.buffer.AddWithGroup(ctx, groupID, uid, &event)
	}

	if event.Resource == domain.ResourcePipelineRun && isTerminalState(event.State) {
		if a.buffer.IsGroupComplete(groupID) {
			return a.flushGroupAndPost(ctx, groupID, event)
		}
		a.log.Info("group member terminal, waiting for remaining members",
			zap.String("group_id", groupID),
			zap.String("uid", uid),
			zap.String("state", string(event.State)),
		)
	}

	return nil
}

// Close releases resources held by the buffer.
// Close releases resources held by the handler.
func (a *Handler) Close() error {
	return a.buffer.Close()
}

// SummaryData is passed to custom templates when rendering pipeline summaries.
type SummaryData struct {
	PipelineName string
	RunName      string
	State        string
	Tasks        []TaskSummary
}

// TaskSummary holds per-task data for template rendering.
type TaskSummary struct {
	Name     string
	State    string
	Emoji    string
	Duration string
}

// flushAndPost retrieves accumulated state and posts aggregate comment.
func (a *Handler) flushAndPost(ctx context.Context, uid string, finalEvent domain.Event) error {
	state, exists := a.buffer.Flush(ctx, uid)
	if !exists || len(state.Tasks) == 0 {
		return nil
	}

	var markdown string
	if a.tmpl != nil {
		markdown = renderWithTemplate(a.tmpl, state, finalEvent)
	} else {
		markdown = generateMarkdown(state)
	}

	aggregateEvent := domain.Event{
		Provider:    finalEvent.Provider,
		Resource:    domain.ResourcePipelineRun,
		RunName:     finalEvent.RunName,
		RunID:       uid,
		Namespace:   finalEvent.Namespace,
		State:       finalEvent.State,
		Context:     "tekton/pipeline-summary",
		Description: markdown,
		CommitSHA:   finalEvent.CommitSHA,
		Repo:        finalEvent.Repo,
		PRNumber:    finalEvent.PRNumber,
	}

	a.log.Info("posting aggregate pipeline summary",
		zap.String("uid", uid),
		zap.Int("task_count", len(state.Tasks)),
	)

	return a.provider.Handle(ctx, aggregateEvent)
}

func (a *Handler) flushGroupAndPost(ctx context.Context, groupID string, finalEvent domain.Event) error {
	state, exists := a.buffer.FlushGroup(ctx, groupID)
	if !exists || len(state.Tasks) == 0 {
		return nil
	}

	var markdown string
	if a.tmpl != nil {
		markdown = renderWithTemplate(a.tmpl, state, finalEvent)
	} else {
		markdown = generateMarkdown(state)
	}

	aggregateEvent := domain.Event{
		Provider:    finalEvent.Provider,
		Resource:    domain.ResourcePipelineRun,
		RunName:     finalEvent.RunName,
		RunID:       finalEvent.RunID,
		Namespace:   finalEvent.Namespace,
		State:       finalEvent.State,
		Context:     "tekton/pipeline-summary",
		Description: markdown,
		CommitSHA:   finalEvent.CommitSHA,
		Repo:        finalEvent.Repo,
		PRNumber:    finalEvent.PRNumber,
	}

	a.log.Info("posting aggregate group pipeline summary",
		zap.String("group_id", groupID),
		zap.Int("task_count", len(state.Tasks)),
	)

	return a.provider.Handle(ctx, aggregateEvent)
}

// renderWithTemplate renders the summary using the configured template.
func renderWithTemplate(tmpl *template.Template, state *RunState, finalEvent domain.Event) string {
	keys := make([]string, 0, len(state.Tasks))
	for name := range state.Tasks {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	tasks := make([]TaskSummary, 0, len(keys))
	for _, name := range keys {
		e := state.Tasks[name]
		tasks = append(tasks, TaskSummary{
			Name:     name,
			State:    string(e.State),
			Emoji:    stateEmoji(e.State),
			Duration: formatDuration(e.StartedAt, e.FinishedAt),
		})
	}

	data := SummaryData{
		PipelineName: finalEvent.PipelineName,
		RunName:      finalEvent.RunName,
		State:        string(finalEvent.State),
		Tasks:        tasks,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return generateMarkdown(state)
	}
	return buf.String()
}

// generateMarkdown creates markdown table from accumulated tasks.
func generateMarkdown(state *RunState) string {
	var buf bytes.Buffer

	buf.WriteString("## Pipeline Summary\n\n")
	buf.WriteString("| Task | Status | Duration |\n")
	buf.WriteString("|------|--------|----------|\n")

	keys := make([]string, 0, len(state.Tasks))
	for name := range state.Tasks {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		event := state.Tasks[name]
		emoji := stateEmoji(event.State)
		duration := formatDuration(event.StartedAt, event.FinishedAt)
		fmt.Fprintf(&buf, "| %s | %s %s | %s |\n", name, emoji, string(event.State), duration)
	}

	return buf.String()
}

// stateEmoji returns an emoji for display in markdown tables.
func stateEmoji(s domain.State) string {
	switch s {
	case domain.StateSuccess:
		return "✅"
	case domain.StateFailure:
		return "❌"
	case domain.StateError:
		return "⚠️"
	case domain.StateCanceled:
		return "🚫"
	case domain.StateRunning:
		return "🔄"
	default:
		return "⏳"
	}
}

// formatDuration formats the elapsed time between start and finish.
func formatDuration(start, finish time.Time) string {
	if start.IsZero() || finish.IsZero() {
		return notAvailable
	}
	d := finish.Sub(start)
	return d.Truncate(time.Second).String()
}

// isTerminalState returns true if the state indicates execution has completed.
func isTerminalState(s domain.State) bool {
	switch s {
	case domain.StateSuccess, domain.StateFailure, domain.StateCanceled, domain.StateError:
		return true
	default:
		return false
	}
}
