package notifier

import (
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func TestShouldNotify(t *testing.T) {
	tests := []struct {
		name     string
		notifyOn []string
		state    domain.State
		want     bool
	}{
		{"empty notifyOn matches all", []string{}, domain.StateSuccess, true},
		{"match success", []string{"success", "failure"}, domain.StateSuccess, true},
		{"match failure", []string{"success", "failure"}, domain.StateFailure, true},
		{"no match pending", []string{"success", "failure"}, domain.StatePending, false},
		{"match error", []string{"error"}, domain.StateError, true},
		{"match canceled", []string{"canceled"}, domain.StateCanceled, true},
		{"match running", []string{"running"}, domain.StateRunning, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldNotify(tt.notifyOn, tt.state)
			if got != tt.want {
				t.Errorf("ShouldNotify(%v, %v) = %v, want %v", tt.notifyOn, tt.state, got, tt.want)
			}
		})
	}
}
