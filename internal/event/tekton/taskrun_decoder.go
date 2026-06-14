// Package tekton implements the Decoder for CloudEvents emitted by
// tekton-events-controller.
//
//nolint:dupl // Similar by design to customrun_decoder.go
package tekton

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// TaskRunDecoder implements event.Decoder for Tekton TaskRun resources.
// Supported types (Tekton v1.12+):
//
//	dev.tekton.event.taskrun.{queued,started,running,unknown,successful,failed}.v1
//
// Payload: { "taskRun": {...} }
type TaskRunDecoder struct{}

// NewTaskRunDecoder creates a new TaskRun decoder.
func NewTaskRunDecoder() *TaskRunDecoder {
	return &TaskRunDecoder{}
}

// Name identifies the decoder.
func (d *TaskRunDecoder) Name() string {
	return decoderNameTaskRun
}

// CanHandle checks if this decoder can handle the event type.
func (d *TaskRunDecoder) CanHandle(eventType string) bool {
	return strings.HasPrefix(eventType, "dev.tekton.event.taskrun.")
}

type taskRunPayload struct {
	TaskRun *runObject `json:"taskRun,omitempty"`
}

// Decode extracts an Envelope from the raw TaskRun event.
func (d *TaskRunDecoder) Decode(raw event.RawEvent) (*event.Envelope, error) {
	if !d.CanHandle(raw.Type) {
		return nil, fmt.Errorf("not a taskrun event: %s", raw.Type)
	}

	var p taskRunPayload
	if err := json.Unmarshal(raw.Data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	if p.TaskRun == nil {
		return nil, errors.New("payload has no taskRun")
	}

	obj := p.TaskRun

	report, err := baseEventFromRun(obj, domain.ResourceTaskRun, raw.Type)
	if err != nil {
		return nil, err
	}

	// Extract TaskName and related labels from standard Tekton labels
	if obj.Metadata.Labels != nil {
		report.TaskName = obj.Metadata.Labels["tekton.dev/task"]
		// PipelineName and PipelineTaskName present when TaskRun is part of a Pipeline
		report.PipelineName = obj.Metadata.Labels["tekton.dev/pipeline"]
		report.PipelineTaskName = obj.Metadata.Labels["tekton.dev/pipelineTask"]
		report.TriggerName = obj.Metadata.Labels["triggers.tekton.dev/trigger"]
		// Check if task is part of finally block
		if obj.Metadata.Labels["tekton.dev/memberOf"] == "finally" {
			report.IsFinallyTask = true
		}
	}

	// Extract TaskDisplayName from embedded spec (optional)
	if obj.Status.TaskSpec != nil && obj.Status.TaskSpec.DisplayName != "" {
		report.TaskDisplayName = obj.Status.TaskSpec.DisplayName
	}

	// Set Context after extracting names/displayNames (prioritize DisplayName > PipelineTaskName > TaskName > RunName)
	report.Context = contextOfTaskRun(obj, report.TaskDisplayName, report.PipelineTaskName, report.TaskName)

	// Extract issue/PR/discussion numbers from annotations (optional)
	applyOptionalNumbers(&report, obj.Metadata.Annotations)

	applyTimestamps(&report, obj)

	// Extract task results (optional)
	if len(obj.Status.TaskResults) > 0 {
		report.Results = make([]domain.Result, 0, len(obj.Status.TaskResults))
		for _, r := range obj.Status.TaskResults {
			report.Results = append(report.Results, domain.Result{
				Name:  r.Name,
				Value: rawMessageToString(r.Value),
			})
		}
	}

	return &event.Envelope{
		CloudEventID:   raw.ID,
		CloudEventType: raw.Type,
		Source:         raw.Source,
		Report:         report,
	}, nil
}

func contextOfTaskRun(obj *runObject, displayName, pipelineTaskName, taskName string) string {
	// Priority: custom annotation > displayName > pipelineTaskName > taskName > runName
	if c := obj.Metadata.Annotations[event.AnnoContext]; c != "" {
		return c
	}
	if displayName != "" {
		return "tekton/" + displayName
	}
	if pipelineTaskName != "" {
		return "tekton/" + pipelineTaskName
	}
	if taskName != "" {
		return "tekton/" + taskName
	}
	return "tekton/task/" + obj.Metadata.Name
}
