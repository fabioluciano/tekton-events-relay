// Package tekton implements shared types and helpers for Tekton CloudEvent decoders.
//
// This file contains shared types (runObject, runMeta, runStatus, condition)
// and helper functions (MapState, descriptionFor) used by TaskRun, PipelineRun,
// and CustomRun decoders.
package tekton

import (
	"strings"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	typePrefix        = "dev.tekton.event."
	descSucceeded     = "Succeeded"
	descQueued        = "Queued"
	descStarted       = "Started"
	descRunning       = "Running"
	descFailed        = "Failed"
	condTypeSucceeded = "Succeeded"

	// Decoder names
	decoderNameTaskRun       = "tekton-taskrun"
	decoderNamePipelineRun   = "tekton-pipelinerun"
	decoderNameCustomRun     = "tekton-customrun"
	decoderNameEventListener = "tekton-eventlistener"
)

// runObject is the common structure for TaskRun, PipelineRun, and CustomRun.
type runObject struct {
	Metadata runMeta   `json:"metadata"`
	Status   runStatus `json:"status"`
}

// runMeta contains metadata fields common to all run types.
type runMeta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	UID         string            `json:"uid"`
	Annotations map[string]string `json:"annotations"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// runStatus contains status fields common to all run types.
type runStatus struct {
	StartTime       *time.Time       `json:"startTime,omitempty"`
	CompletionTime  *time.Time       `json:"completionTime,omitempty"`
	Conditions      []condition      `json:"conditions,omitempty"`
	PipelineResults []runResult      `json:"pipelineResults,omitempty"` // PipelineRun results
	TaskResults     []runResult      `json:"taskResults,omitempty"`     // TaskRun results
	PipelineSpec    *pipelineSpec    `json:"pipelineSpec,omitempty"`    // Embedded pipeline spec (PipelineRun)
	TaskSpec        *taskSpec        `json:"taskSpec,omitempty"`        // Embedded task spec (TaskRun)
	ChildReferences []childReference `json:"childReferences,omitempty"` // Child TaskRuns/CustomRuns (PipelineRun)
}

// childReference represents a reference to a child TaskRun or CustomRun.
type childReference struct {
	APIVersion       string `json:"apiVersion"`
	Kind             string `json:"kind"`
	Name             string `json:"name"`
	PipelineTaskName string `json:"pipelineTaskName"`
}

// runResult represents a Tekton result (name/value pair).
type runResult struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// pipelineSpec contains fields we care about from the embedded pipeline spec.
type pipelineSpec struct {
	DisplayName string `json:"displayName,omitempty"`
}

// taskSpec contains fields we care about from the embedded task spec.
type taskSpec struct {
	DisplayName string `json:"displayName,omitempty"`
}

// condition represents a Tekton condition.
type condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// MapState translates Tekton event type to the neutral vocabulary.
// Exported for use by all decoders and tests.
func MapState(eventType string) domain.State {
	switch {
	case strings.HasSuffix(eventType, ".queued.v1"),
		strings.HasSuffix(eventType, ".started.v1"):
		return domain.StatePending
	case strings.HasSuffix(eventType, ".running.v1"),
		strings.HasSuffix(eventType, ".unknown.v1"):
		return domain.StateRunning
	case strings.HasSuffix(eventType, ".successful.v1"):
		return domain.StateSuccess
	case strings.HasSuffix(eventType, ".failed.v1"):
		return domain.StateFailure
	}
	return domain.StatePending
}

// descriptionFor extracts a human-readable description from the run object.
// Exported for use by all decoders.
func descriptionFor(obj *runObject, eventType string) string {
	for _, c := range obj.Status.Conditions {
		if c.Type == condTypeSucceeded && c.Message != "" {
			if len(c.Message) <= 140 {
				return c.Message
			}
			return c.Message[:137] + "..."
		}
	}
	switch {
	case strings.HasSuffix(eventType, ".queued.v1"):
		return descQueued
	case strings.HasSuffix(eventType, ".started.v1"):
		return descStarted
	case strings.HasSuffix(eventType, ".running.v1"):
		return descRunning
	case strings.HasSuffix(eventType, ".successful.v1"):
		return descSucceeded
	case strings.HasSuffix(eventType, ".failed.v1"):
		return descFailed
	}
	return ""
}
