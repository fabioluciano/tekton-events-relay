// Package cehttp provides CloudEvents parsing using the official SDK.
//
// Supports both binary-mode and structured-mode CloudEvents as defined in:
// https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/bindings/http-protocol-binding.md
package cehttp

import (
	"net/http"
	"time"

	"github.com/cloudevents/sdk-go/v2/binding"
	sdkhttp "github.com/cloudevents/sdk-go/v2/protocol/http"
)

// Event is the minimal representation of a received CloudEvent.
type Event struct {
	ID          string
	Type        string
	Source      string
	SpecVersion string
	Subject     string
	Time        string
	Data        []byte
}

// FromRequest extracts an Event from an HTTP request.
// Supports both binary-mode and structured-mode CloudEvents.
// Returns error if the request is not a valid CloudEvent.
func FromRequest(r *http.Request) (*Event, error) {
	msg := sdkhttp.NewMessageFromHttpRequest(r)
	defer func() { _ = msg.Finish(nil) }()

	ce, err := binding.ToEvent(r.Context(), msg)
	if err != nil {
		return nil, err
	}
	if err := ce.Validate(); err != nil {
		return nil, err
	}

	timeStr := ""
	if !ce.Time().IsZero() {
		timeStr = ce.Time().Format(time.RFC3339)
	}

	return &Event{
		ID:          ce.ID(),
		Type:        ce.Type(),
		Source:      ce.Source(),
		SpecVersion: ce.SpecVersion(),
		Subject:     ce.Subject(),
		Time:        timeStr,
		Data:        ce.DataEncoded,
	}, nil
}
