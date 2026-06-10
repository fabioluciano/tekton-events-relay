package scm

import (
	"fmt"
	"strings"
)

// Comment posting modes. In upsert mode the handler embeds an invisible
// HTML marker in the comment body and edits the existing marked comment
// on subsequent events instead of creating a new one. This makes comment
// actions idempotent across retries, restarts and multiple replicas: the
// deduplication state lives in the PR itself.
const (
	ModeCreate = "create"
	ModeUpsert = "upsert"
)

// NormalizeMode validates a configured comment mode, defaulting to create.
func NormalizeMode(mode string) (string, error) {
	switch mode {
	case "", ModeCreate:
		return ModeCreate, nil
	case ModeUpsert:
		return ModeUpsert, nil
	default:
		return "", fmt.Errorf("invalid comment mode %q (must be %q or %q)", mode, ModeCreate, ModeUpsert)
	}
}

// Marker builds the hidden HTML marker identifying a relay-managed comment
// for a given run and action type. HTML comments are not rendered by any
// supported SCM, so the marker is invisible to readers.
func Marker(runID, action string) string {
	return fmt.Sprintf("<!-- tekton-events-relay:%s:%s -->", runID, action)
}

// WithMarker prefixes body with the marker on its own line.
func WithMarker(marker, body string) string {
	return marker + "\n" + body
}

// HasMarker reports whether a comment body carries the given marker.
func HasMarker(body, marker string) bool {
	return strings.Contains(body, marker)
}
