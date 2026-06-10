// Package tekton implements the Decoder for CloudEvents emitted by
// tekton-events-controller.
//
//nolint:dupl // Similar by design to taskrun_decoder.go
package tekton

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// CustomRunDecoder implements event.Decoder for Tekton CustomRun resources.
// Supported types (Tekton v1.12+):
//
//	dev.tekton.event.customrun.{queued,started,running,unknown,successful,failed}.v1
//
// Payload: { "customRun": {...} }
type CustomRunDecoder struct{}

// NewCustomRunDecoder creates a new CustomRun decoder.
func NewCustomRunDecoder() *CustomRunDecoder {
	return &CustomRunDecoder{}
}

// Name identifies the decoder.
func (d *CustomRunDecoder) Name() string {
	return decoderNameCustomRun
}

// CanHandle checks if this decoder can handle the event type.
func (d *CustomRunDecoder) CanHandle(eventType string) bool {
	return strings.HasPrefix(eventType, "dev.tekton.event.customrun.")
}

type customRunPayload struct {
	CustomRun *runObject `json:"customRun,omitempty"`
}

// Decode extracts an Envelope from the raw CustomRun event.
func (d *CustomRunDecoder) Decode(raw event.RawEvent) (*event.Envelope, error) {
	if !d.CanHandle(raw.Type) {
		return nil, fmt.Errorf("not a customrun event: %s", raw.Type)
	}

	var p customRunPayload
	if err := json.Unmarshal(raw.Data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	if p.CustomRun == nil {
		return nil, errors.New("payload has no customRun")
	}

	obj := p.CustomRun

	report, err := baseEventFromRun(obj, domain.ResourceCustomRun, raw.Type)
	if err != nil {
		return nil, err
	}

	report.Context = contextOfCustomRun(obj)

	// Extract TaskName and PipelineName from standard Tekton labels (set automatically by controller)
	if obj.Metadata.Labels != nil {
		report.TaskName = obj.Metadata.Labels["tekton.dev/task"]
		report.PipelineName = obj.Metadata.Labels["tekton.dev/pipeline"]
	}

	// Extract issue/PR/discussion numbers from annotations (optional)
	applyOptionalNumbers(&report, obj.Metadata.Annotations)

	applyTimestamps(&report, obj)

	return &event.Envelope{
		CloudEventID:   raw.ID,
		CloudEventType: raw.Type,
		Source:         raw.Source,
		Report:         report,
	}, nil
}

func contextOfCustomRun(obj *runObject) string {
	if c := obj.Metadata.Annotations[event.AnnoContext]; c != "" {
		return c
	}
	return "tekton/customrun/" + obj.Metadata.Name
}
