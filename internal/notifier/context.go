package notifier

// contextKey is an unexported type for context values to avoid collisions.
type contextKey string

// CloudEventIDKey is the context key for the CloudEvent ID. The pipeline
// dispatcher sets this so wrapped handlers (e.g. DedupeHandler) can
// access the CloudEvent ID that triggered the notification.
const CloudEventIDKey contextKey = "ce_id"
