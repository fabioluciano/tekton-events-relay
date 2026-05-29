// Package tekton implements the Decoder for CloudEvents emitted by
// tekton-events-controller.
//
// Supported types (Tekton v1.12+):
//
//	dev.tekton.event.{taskrun,pipelinerun}.{queued,started,running,unknown,successful,failed}.v1
//
// Payload: { "taskRun": {...} } or { "pipelineRun": {...} }
package tekton

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/stringutil"
)

const (
	typePrefix        = "dev.tekton.event."
	descSucceeded     = "Succeeded"
	descQueued        = "Queued"
	descStarted       = "Started"
	descRunning       = "Running"
	descFailed        = "Failed"
	condTypeSucceeded = "Succeeded"
)

// Decoder implements event.Decoder for Tekton.
type Decoder struct{}

func New() *Decoder { return &Decoder{} }

func (d *Decoder) Name() string { return "tekton" }

func (d *Decoder) CanHandle(eventType string) bool {
	return strings.HasPrefix(eventType, typePrefix)
}

type payload struct {
	TaskRun     *runObject `json:"taskRun,omitempty"`
	PipelineRun *runObject `json:"pipelineRun,omitempty"`
}

type runObject struct {
	Metadata runMeta   `json:"metadata"`
	Status   runStatus `json:"status"`
}

type runMeta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	UID         string            `json:"uid"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type runStatus struct {
	StartTime      *time.Time  `json:"startTime,omitempty"`
	CompletionTime *time.Time  `json:"completionTime,omitempty"`
	Conditions     []condition `json:"conditions,omitempty"`
}

type condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func (d *Decoder) Decode(raw event.RawEvent) (*event.Envelope, error) {
	if !d.CanHandle(raw.Type) {
		return nil, fmt.Errorf("not a tekton event: %s", raw.Type)
	}

	var p payload
	if err := json.Unmarshal(raw.Data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	var obj *runObject
	var resource domain.Resource
	switch {
	case p.PipelineRun != nil:
		obj = p.PipelineRun
		resource = domain.ResourcePipelineRun
	case p.TaskRun != nil:
		obj = p.TaskRun
		resource = domain.ResourceTaskRun
	default:
		return nil, errors.New("payload has neither taskRun nor pipelineRun")
	}

	provider := obj.Metadata.Labels[event.LabelProvider]
	if provider == "" {
		return nil, fmt.Errorf("missing label %s on %s/%s",
			event.LabelProvider, obj.Metadata.Namespace, obj.Metadata.Name)
	}

	sha := obj.Metadata.Annotations[event.AnnoCommitSHA]
	if sha == "" {
		return nil, fmt.Errorf("missing annotation %s", event.AnnoCommitSHA)
	}

	report := domain.Event{
		Provider:   provider,
		Resource:   resource,
		APIBaseURL: obj.Metadata.Annotations[event.AnnoAPIBaseURL],
		Repo: domain.Repo{
			Owner:     obj.Metadata.Annotations[event.AnnoRepoOwner],
			Name:      obj.Metadata.Annotations[event.AnnoRepoName],
			ID:        obj.Metadata.Annotations[event.AnnoRepoID],
			Workspace: obj.Metadata.Annotations[event.AnnoRepoWorkspace],
			Project:   obj.Metadata.Annotations[event.AnnoRepoProject],
			Org:       obj.Metadata.Annotations[event.AnnoRepoOrg],
		},
		CommitSHA:   sha,
		State:       MapState(raw.Type),
		Context:     contextOf(obj, raw.Type),
		Description: descriptionFor(obj, raw.Type),
		RunName:     obj.Metadata.Name,
		RunID:       obj.Metadata.UID,
		Namespace:   obj.Metadata.Namespace,
	}

	// Extract issue/PR numbers from annotations (optional)
	if issueStr := obj.Metadata.Annotations[event.AnnoIssueNumber]; issueStr != "" {
		if n, err := strconv.Atoi(issueStr); err == nil {
			report.IssueNumber = &n
		}
	}
	if prStr := obj.Metadata.Annotations[event.AnnoPRNumber]; prStr != "" {
		if n, err := strconv.Atoi(prStr); err == nil {
			report.PRNumber = &n
		}
	}

	if obj.Status.StartTime != nil {
		report.StartedAt = *obj.Status.StartTime
	}
	if obj.Status.CompletionTime != nil {
		report.FinishedAt = *obj.Status.CompletionTime
	}

	return &event.Envelope{
		CloudEventID:   raw.ID,
		CloudEventType: raw.Type,
		Source:         raw.Source,
		Report:         report,
	}, nil
}

// MapState translates Tekton event type to the neutral vocabulary.
// Exported for use in tests.
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

func contextOf(obj *runObject, eventType string) string {
	if c := obj.Metadata.Annotations[event.AnnoContext]; c != "" {
		return c
	}
	if strings.Contains(eventType, ".pipelinerun.") {
		return "tekton/" + obj.Metadata.Name
	}
	return "tekton/task/" + obj.Metadata.Name
}

func descriptionFor(obj *runObject, eventType string) string {
	for _, c := range obj.Status.Conditions {
		if c.Type == condTypeSucceeded && c.Message != "" {
			return stringutil.TruncateWithEllipsis(c.Message, 140)
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

