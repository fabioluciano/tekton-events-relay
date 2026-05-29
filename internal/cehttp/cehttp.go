// Package cehttp implements a minimalist CloudEvents parser in "binary" mode
// (the only mode used by tekton-events-controller).
//
// Follows CloudEvents v1.0 spec:
// https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/bindings/http-protocol-binding.md#3-http-binding-modes
//
// In binary mode, CloudEvent attributes come as HTTP headers
// (Ce-Id, Ce-Type, Ce-Source, Ce-Specversion, Ce-Subject, Ce-Time) and the
// event payload comes in the body. We don't need the complete SDK just for this.
package cehttp

import (
	"errors"
	"io"
	"net/http"
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

// FromRequest extracts an Event from an HTTP request in binary mode.
// Returns error if required headers are missing.
func FromRequest(r *http.Request) (*Event, error) {
	id := r.Header.Get("Ce-Id")
	if id == "" {
		return nil, errors.New("missing Ce-Id header")
	}
	typ := r.Header.Get("Ce-Type")
	if typ == "" {
		return nil, errors.New("missing Ce-Type header")
	}
	src := r.Header.Get("Ce-Source")
	if src == "" {
		return nil, errors.New("missing Ce-Source header")
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	return &Event{
		ID:          id,
		Type:        typ,
		Source:      src,
		SpecVersion: r.Header.Get("Ce-Specversion"),
		Subject:     r.Header.Get("Ce-Subject"),
		Time:        r.Header.Get("Ce-Time"),
		Data:        body,
	}, nil
}
