package sourcehut

import "github.com/fabioluciano/tekton-events-relay/internal/domain"

const providerName = "sourcehut"

func exitFor(s domain.State) int {
	if s == domain.StateFailure || s == domain.StateError {
		return 1
	}
	return 0
}
