package tekton

import (
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// TestDecoderRouting verifies that each decoder claims only its intended event types
func TestDecoderRouting(t *testing.T) {
	decoders := []struct {
		name    string
		decoder event.Decoder
	}{
		{decoderNameTaskRun, NewTaskRunDecoder()},
		{decoderNamePipelineRun, NewPipelineRunDecoder()},
		{decoderNameCustomRun, NewCustomRunDecoder()},
		{decoderNameEventListener, NewEventListenerDecoder()},
	}

	testCases := []struct {
		eventType       string
		expectedDecoder string
		shouldBeHandled bool
	}{
		{typeTaskSuccessful, decoderNameTaskRun, true},
		{typePipelineSuccessful, decoderNamePipelineRun, true},
		{typeCustomRunSuccessful, decoderNameCustomRun, true},
		{typeCustomRunFailed, decoderNameCustomRun, true},
		{typeEventListenerStarted, decoderNameEventListener, true},
		{typeEventListenerDone, decoderNameEventListener, true},
		{"io.example.foreign.v1", "", false},
	}

	for _, tc := range testCases {
		t.Run(tc.eventType, func(t *testing.T) {
			var matchedDecoder string
			matchCount := 0

			for _, d := range decoders {
				if d.decoder.CanHandle(tc.eventType) {
					matchedDecoder = d.name
					matchCount++
				}
			}

			if !tc.shouldBeHandled {
				if matchCount > 0 {
					t.Errorf("Event %q should not be handled by any decoder, but %s claimed it",
						tc.eventType, matchedDecoder)
				}
				return
			}

			if matchCount == 0 {
				t.Errorf("Event %q should be handled by %s, but no decoder claimed it",
					tc.eventType, tc.expectedDecoder)
				return
			}

			if matchCount > 1 {
				t.Errorf("Event %q claimed by multiple decoders (ambiguous routing)",
					tc.eventType)
				return
			}

			if matchedDecoder != tc.expectedDecoder {
				t.Errorf("Event %q: got decoder %s, want %s",
					tc.eventType, matchedDecoder, tc.expectedDecoder)
			}
		})
	}
}
