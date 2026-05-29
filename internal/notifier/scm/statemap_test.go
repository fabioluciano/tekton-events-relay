package scm_test

import (
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

func TestStateMap_Map(t *testing.T) {
	testMap := scm.StateMap{
		domain.StatePending: "pending",
		domain.StateRunning: "in_progress",
		domain.StateSuccess: "success",
		domain.StateFailure: "failed",
	}

	tests := []struct {
		name     string
		state    domain.State
		fallback string
		want     string
	}{
		{
			name:     "mapped state returns correct value",
			state:    domain.StatePending,
			fallback: "unknown",
			want:     "pending",
		},
		{
			name:     "unmapped state returns fallback",
			state:    domain.StateError,
			fallback: "unknown",
			want:     "unknown",
		},
		{
			name:     "running state with custom mapping",
			state:    domain.StateRunning,
			fallback: "pending",
			want:     "in_progress",
		},
		{
			name:     "success state",
			state:    domain.StateSuccess,
			fallback: "unknown",
			want:     "success",
		},
		{
			name:     "failure state",
			state:    domain.StateFailure,
			fallback: "unknown",
			want:     "failed",
		},
		{
			name:     "empty fallback for unmapped",
			state:    domain.StateCanceled,
			fallback: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := testMap.Map(tt.state, tt.fallback)
			if got != tt.want {
				t.Errorf("StateMap.Map() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStateMap_EmptyMap(t *testing.T) {
	emptyMap := scm.StateMap{}
	got := emptyMap.Map(domain.StateSuccess, "default")
	if got != "default" {
		t.Errorf("empty map should return fallback, got %v", got)
	}
}
