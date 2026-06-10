// Package tekton implements the Decoder for CloudEvents emitted by
// tekton-events-controller.
package tekton

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// EventListenerDecoder implements event.Decoder for Tekton EventListener resources.
// Supported types (Tekton v1.12+):
//
//	dev.tekton.event.triggers.{started,successful,failed,done}.v1
//
// started.v1 payload: incoming webhook HTTP headers (map[string][]string)
// successful/failed.v1 payload: trigger lifecycle data
// done.v1 payload: null
type EventListenerDecoder struct{}

const (
	canonicalEventPullRequest  = "pull_request"
	canonicalEventIssueComment = "issue_comment"
)

// NewEventListenerDecoder creates a new EventListener decoder.
func NewEventListenerDecoder() *EventListenerDecoder {
	return &EventListenerDecoder{}
}

// Name identifies the decoder.
func (d *EventListenerDecoder) Name() string {
	return decoderNameEventListener
}

// CanHandle checks if this decoder can handle the event type.
func (d *EventListenerDecoder) CanHandle(eventType string) bool {
	return strings.HasPrefix(eventType, "dev.tekton.event.triggers.")
}

type eventListenerPayload struct {
	EventListener    string `json:"eventListener"`
	Namespace        string `json:"namespace"`
	EventListenerUID string `json:"eventListenerUID"`
	EventID          string `json:"eventID"`
}

// Decode extracts an Envelope from the raw EventListener event.
func (d *EventListenerDecoder) Decode(raw event.RawEvent) (*event.Envelope, error) {
	if !d.CanHandle(raw.Type) {
		return nil, fmt.Errorf("not an eventlistener event: %s", raw.Type)
	}

	// started.v1: payload is the incoming webhook HTTP headers
	if strings.HasSuffix(raw.Type, ".started.v1") {
		return d.decodeStarted(raw)
	}

	// done.v1: null payload — no useful data
	if strings.HasSuffix(raw.Type, ".done.v1") {
		return nil, fmt.Errorf("done event carries no data")
	}

	// successful.v1 / failed.v1: trigger lifecycle payload
	return d.decodeLifecycle(raw)
}

func (d *EventListenerDecoder) decodeStarted(raw event.RawEvent) (*event.Envelope, error) {
	if len(raw.Data) == 0 || string(raw.Data) == "null" {
		return nil, fmt.Errorf("started event has no payload")
	}

	var headers map[string][]string
	if err := json.Unmarshal(raw.Data, &headers); err != nil {
		return nil, fmt.Errorf("unmarshal headers: %w", err)
	}

	provider, scmEventType := extractSCMContext(headers)
	if provider == "" {
		return nil, fmt.Errorf("no recognised SCM webhook headers found")
	}

	elName := extractLastPathSegment(raw.Source)

	report := domain.Event{
		Resource:          domain.ResourceEventListener,
		EventListenerName: elName,
		RunName:           elName,
		RunID:             raw.ID,
		State:             domain.StateRunning,
		Context:           "tekton/eventlistener/" + elName,
		Description:       "EventListener processing started",
		Provider:          provider,
		SCMEventType:      scmEventType,
	}

	return &event.Envelope{
		CloudEventID:   raw.ID,
		CloudEventType: raw.Type,
		Source:         raw.Source,
		Report:         report,
	}, nil
}

func (d *EventListenerDecoder) decodeLifecycle(raw event.RawEvent) (*event.Envelope, error) {
	var p eventListenerPayload
	if err := json.Unmarshal(raw.Data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	if p.EventListener == "" {
		return nil, fmt.Errorf("missing eventListener field")
	}
	if p.Namespace == "" {
		return nil, fmt.Errorf("missing namespace field")
	}
	if p.EventListenerUID == "" {
		return nil, fmt.Errorf("missing eventListenerUID field")
	}

	report := domain.Event{
		Resource:          domain.ResourceEventListener,
		EventListenerName: p.EventListener,
		RunName:           p.EventListener,
		RunID:             p.EventListenerUID,
		Namespace:         p.Namespace,
		State:             mapEventListenerState(raw.Type),
		Context:           "tekton/eventlistener/" + p.EventListener,
		Description:       descriptionForEventListener(raw.Type),
	}

	return &event.Envelope{
		CloudEventID:   raw.ID,
		CloudEventType: raw.Type,
		Source:         raw.Source,
		Report:         report,
	}, nil
}

// extractSCMContext detects the SCM provider and event type from webhook headers.
// Returns provider name and a normalized canonical event type.
//
// Canonical event types:
//   - "push"           — push to branch/tag
//   - "pull_request"   — PR/MR opened, updated, merged
//   - "issues"         — issue opened, closed, updated
//   - "issue_comment"  — comment on issue, PR, or commit
//
// Note: X-Gitea-Event is checked before X-Github-Event because Gitea sends
// both headers for compatibility, and we want to identify Gitea correctly.
func extractSCMContext(headers map[string][]string) (provider, eventType string) {
	first := func(key string) string {
		if vals := headers[key]; len(vals) > 0 {
			return vals[0]
		}
		return ""
	}

	// Gitea before GitHub — Gitea sends X-GitHub-Event for compatibility
	if v := first("X-Gitea-Event"); v != "" {
		return "gitea", normalizeGiteaEvent(v)
	}
	if v := first("X-Github-Event"); v != "" {
		return "github", v // GitHub values are already canonical
	}
	if v := first("X-Gitlab-Event"); v != "" {
		return "gitlab", normalizeGitLabEvent(v)
	}
	if v := first("X-Event-Key"); v != "" {
		return "bitbucket", normalizeBitbucketEvent(v)
	}
	return "", ""
}

// normalizeGitLabEvent maps GitLab X-Gitlab-Event values to canonical types.
// GitLab uses human-readable strings like "Push Hook", "Merge Request Hook".
func normalizeGitLabEvent(v string) string {
	switch v {
	case "Push Hook", "Tag Push Hook":
		return "push"
	case "Merge Request Hook":
		return canonicalEventPullRequest
	case "Issue Hook":
		return "issues"
	case "Note Hook":
		return canonicalEventIssueComment
	default:
		return v
	}
}

// normalizeBitbucketEvent maps Bitbucket X-Event-Key values to canonical types.
// Bitbucket uses namespaced keys like "repo:push", "pullrequest:created".
func normalizeBitbucketEvent(v string) string {
	switch {
	case v == "repo:push":
		return "push"
	case strings.HasPrefix(v, "pullrequest:comment_"):
		return canonicalEventIssueComment
	case strings.HasPrefix(v, "pullrequest:"):
		return canonicalEventPullRequest
	case v == "issue:comment_created":
		return canonicalEventIssueComment
	case strings.HasPrefix(v, "issue:"):
		return "issues"
	default:
		return v
	}
}

// normalizeGiteaEvent maps Gitea X-Gitea-Event values to canonical types.
// Gitea uses GitHub-compatible names but some differ slightly.
func normalizeGiteaEvent(v string) string {
	switch v {
	case "pull_request", "pull_request_review", "pull_request_review_comment":
		return canonicalEventPullRequest
	case "issues":
		return "issues"
	case "issue_comment":
		return canonicalEventIssueComment
	case "push":
		return "push"
	default:
		return v
	}
}

// extractLastPathSegment returns the last non-empty segment of a URL path.
func extractLastPathSegment(source string) string {
	parts := strings.Split(source, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return source
}

func mapEventListenerState(eventType string) domain.State {
	switch {
	case strings.HasSuffix(eventType, ".started.v1"):
		return domain.StateRunning
	case strings.HasSuffix(eventType, ".successful.v1"):
		return domain.StateSuccess
	case strings.HasSuffix(eventType, ".failed.v1"):
		return domain.StateFailure
	case strings.HasSuffix(eventType, ".done.v1"):
		return domain.StateDone
	}
	return domain.StatePending
}

func descriptionForEventListener(eventType string) string {
	switch {
	case strings.HasSuffix(eventType, ".started.v1"):
		return "EventListener processing started"
	case strings.HasSuffix(eventType, ".successful.v1"):
		return "EventListener processing successful"
	case strings.HasSuffix(eventType, ".failed.v1"):
		return "EventListener processing failed"
	case strings.HasSuffix(eventType, ".done.v1"):
		return "EventListener processing done"
	}
	return ""
}
