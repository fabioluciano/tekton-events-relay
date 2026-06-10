package scm_test

import (
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

const (
	testPending    = "pending"
	testInProgress = "in_progress"
	testSuccess    = "success"
	testFailed     = "failed"
	testUnknown    = "unknown"
	testDefault    = "default"
)

func TestStateMap_Map(t *testing.T) {
	testMap := scm.StateMap{
		domain.StatePending: testPending,
		domain.StateRunning: testInProgress,
		domain.StateSuccess: testSuccess,
		domain.StateFailure: testFailed,
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
			fallback: testUnknown,
			want:     testPending,
		},
		{
			name:     "unmapped state returns fallback",
			state:    domain.StateError,
			fallback: testUnknown,
			want:     testUnknown,
		},
		{
			name:     "running state with custom mapping",
			state:    domain.StateRunning,
			fallback: testPending,
			want:     testInProgress,
		},
		{
			name:     "success state",
			state:    domain.StateSuccess,
			fallback: testUnknown,
			want:     testSuccess,
		},
		{
			name:     "failure state",
			state:    domain.StateFailure,
			fallback: testUnknown,
			want:     testFailed,
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
	got := emptyMap.Map(domain.StateSuccess, testDefault)
	if got != testDefault {
		t.Errorf("empty map should return fallback, got %v", got)
	}
}
