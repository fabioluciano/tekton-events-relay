// Package tekton implements the Decoder for CloudEvents emitted by
// tekton-events-controller.
package tekton

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// PipelineRunDecoder implements event.Decoder for Tekton PipelineRun resources.
// Supported types (Tekton v1.12+):
//
//	dev.tekton.event.pipelinerun.{queued,started,running,unknown,successful,failed}.v1
//
// Payload: { "pipelineRun": {...} }
type PipelineRunDecoder struct{}

// NewPipelineRunDecoder creates a new PipelineRun decoder.
func NewPipelineRunDecoder() *PipelineRunDecoder {
	return &PipelineRunDecoder{}
}

// Name identifies the decoder.
func (d *PipelineRunDecoder) Name() string {
	return decoderNamePipelineRun
}

// CanHandle checks if this decoder can handle the event type.
func (d *PipelineRunDecoder) CanHandle(eventType string) bool {
	return strings.HasPrefix(eventType, "dev.tekton.event.pipelinerun.")
}

type pipelineRunPayload struct {
	PipelineRun *runObject `json:"pipelineRun,omitempty"`
}

// Decode extracts an Envelope from the raw PipelineRun event.
func (d *PipelineRunDecoder) Decode(raw event.RawEvent) (*event.Envelope, error) {
	if !d.CanHandle(raw.Type) {
		return nil, fmt.Errorf("not a pipelinerun event: %s", raw.Type)
	}

	var p pipelineRunPayload
	if err := json.Unmarshal(raw.Data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	if p.PipelineRun == nil {
		return nil, errors.New("payload has no pipelineRun")
	}

	obj := p.PipelineRun

	report, err := baseEventFromRun(obj, domain.ResourcePipelineRun, raw.Type)
	if err != nil {
		return nil, err
	}

	// Extract PipelineName and TriggerName from standard Tekton labels
	if obj.Metadata.Labels != nil {
		report.PipelineName = obj.Metadata.Labels["tekton.dev/pipeline"]
		report.TriggerName = obj.Metadata.Labels["triggers.tekton.dev/trigger"]
	}

	// Extract PipelineDisplayName from embedded spec (optional)
	if obj.Status.PipelineSpec != nil && obj.Status.PipelineSpec.DisplayName != "" {
		report.PipelineDisplayName = obj.Status.PipelineSpec.DisplayName
	}

	// Set Context after extracting names/displayNames (prioritize DisplayName > Name > RunName)
	report.Context = contextOfPipelineRun(obj, report.PipelineDisplayName, report.PipelineName)

	// Extract issue/PR/discussion numbers from annotations (optional)
	applyOptionalNumbers(&report, obj.Metadata.Annotations)

	applyTimestamps(&report, obj)

	// Extract pipeline results (optional)
	if len(obj.Status.PipelineResults) > 0 {
		report.Results = make([]domain.Result, 0, len(obj.Status.PipelineResults))
		for _, r := range obj.Status.PipelineResults {
			report.Results = append(report.Results, domain.Result{
				Name:  r.Name,
				Value: rawMessageToString(r.Value),
			})
		}
	}

	// Extract task count from childReferences
	report.TaskCount = len(obj.Status.ChildReferences)

	return &event.Envelope{
		CloudEventID:   raw.ID,
		CloudEventType: raw.Type,
		Source:         raw.Source,
		Report:         report,
	}, nil
}

func contextOfPipelineRun(obj *runObject, displayName, pipelineName string) string {
	// Priority: custom annotation > displayName > pipelineName > runName
	if c := obj.Metadata.Annotations[event.AnnoContext]; c != "" {
		return c
	}
	if displayName != "" {
		return "tekton/" + displayName
	}
	if pipelineName != "" {
		return "tekton/" + pipelineName
	}
	return "tekton/" + obj.Metadata.Name
}
