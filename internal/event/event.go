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

// Convention of labels/annotations common to ALL engines.
// Each decoder reads these fields from the input payload.
const (
	LabelProvider = "scm.provider"

	AnnoRepoOwner     = "scm.repo-owner"
	AnnoRepoName      = "scm.repo-name"
	AnnoRepoID        = "scm.repo-id"
	AnnoRepoWorkspace = "scm.repo-workspace"
	AnnoRepoProject   = "scm.repo-project"
	AnnoRepoOrg       = "scm.repo-org"
	AnnoCommitSHA     = "scm.commit-sha"
	AnnoAPIBaseURL    = "scm.api-base-url"
	AnnoContext       = "scm.context"

	// Issue/PR linking annotations for action handlers (comments, labels)
	AnnoIssueNumber = "tekton-events-relay.dev/issue-number"
	AnnoPRNumber    = "tekton-events-relay.dev/pr-number"
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
