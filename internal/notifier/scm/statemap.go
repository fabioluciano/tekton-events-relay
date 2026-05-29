package scm

import "github.com/fabioluciano/tekton-events-relay/internal/domain"

// StateMap maps domain.State values to provider-specific state strings.
// Each SCM provider declares its own StateMap with the appropriate mappings.
type StateMap map[domain.State]string

// Map returns the provider-specific state string for the given domain state.
// If the state is not found in the map, returns the fallback string.
func (m StateMap) Map(s domain.State, fallback string) string {
	if v, ok := m[s]; ok {
		return v
	}
	return fallback
}
