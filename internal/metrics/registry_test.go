package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewCollectors_DuplicateRegistrationPanics(t *testing.T) {
	reg := prometheus.NewRegistry()

	// First registration should succeed
	c := NewCollectors(reg)
	if c == nil {
		t.Fatal("expected non-nil Collectors on first registration")
	}

	// Second registration on the same registry should panic due to MustRegister
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate registration, got none")
		}
	}()

	_ = NewCollectors(reg)
}
