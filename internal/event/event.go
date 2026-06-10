// Package event defines the types shared between decoders (Strategy)
// and the registry that resolves which decoder to use for each CloudEvent.
//
// Each pipeline engine (Tekton, Jenkins, ...) has its own payload schema
// and its own event types. Instead of a single monolithic parser, we expose
// a Decoder interface and let each subpackage implement it.
package event

import (
	"fmt"
	"sync"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// Convention of annotations common to ALL engines.
// Each decoder reads these fields from the input payload.
const (
	AnnoProvider = "tekton.dev/tekton-events-relay.scm.provider"

	AnnoRepoOwner     = "tekton.dev/tekton-events-relay.scm.repo-owner"
	AnnoRepoName      = "tekton.dev/tekton-events-relay.scm.repo-name"
	AnnoRepoID        = "tekton.dev/tekton-events-relay.scm.repo-id"
	AnnoRepoWorkspace = "tekton.dev/tekton-events-relay.scm.repo-workspace"
	AnnoRepoProject   = "tekton.dev/tekton-events-relay.scm.repo-project"
	AnnoRepoOrg       = "tekton.dev/tekton-events-relay.scm.repo-org"
	AnnoCommitSHA     = "tekton.dev/tekton-events-relay.scm.commit-sha"
	AnnoAPIBaseURL    = "tekton.dev/tekton-events-relay.scm.api-base-url"
	AnnoContext       = "tekton.dev/tekton-events-relay.scm.context"

	// Issue/PR/Discussion linking annotations for action handlers (comments, labels)
	AnnoIssueNumber      = "tekton.dev/tekton-events-relay.scm.issue-number"
	AnnoPRNumber         = "tekton.dev/tekton-events-relay.scm.pr-number"
	AnnoDiscussionNumber = "tekton.dev/tekton-events-relay.scm.discussion-number"
)

// RawEvent is the decoder input, decoupled from the CloudEvents SDK.
type RawEvent struct {
	ID     string
	Type   string
	Source string
	Data   []byte
}

// Envelope is the decode result: neutral report + CloudEvent metadata
// preserved for downstream handlers (Deduper uses CloudEventID).
type Envelope struct {
	CloudEventID   string
	CloudEventType string
	Source         string
	Report         domain.Event
}

// Decoder is the Strategy interface. Each pipeline engine implements
// a version (tekton, jenkins, ...).
type Decoder interface {
	// Name identifies the decoder (for logs/metrics).
	Name() string
	// CanHandle answers whether this decoder can handle the CloudEvent type.
	// Typically checks the prefix: "dev.tekton.event.", etc.
	CanHandle(eventType string) bool
	// Decode extracts an Envelope from the raw event.
	Decode(raw RawEvent) (*Envelope, error)
}

// Registry maintains the ordered list of decoders and resolves which to use.
// Thread-safe.
type Registry struct {
	mu       sync.RWMutex
	decoders []Decoder
}

// NewRegistry creates a new decoder registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a decoder. The registration order is the attempt order
// in Find — the first CanHandle=true wins.
func (r *Registry) Register(d Decoder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decoders = append(r.decoders, d)
}

// Find resolves the decoder for the given event type.
func (r *Registry) Find(eventType string) (Decoder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, d := range r.decoders {
		if d.CanHandle(eventType) {
			return d, nil
		}
	}
	return nil, fmt.Errorf("no decoder registered for event type %q", eventType)
}

// Names lists the registered decoders (useful for health/log).
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.decoders))
	for _, d := range r.decoders {
		out = append(out, d.Name())
	}
	return out
}
