// Package secrets provides utilities for resolving secret references.
package secrets

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// sanitizePath validates that a path component does not contain traversal sequences.
func sanitizePath(component string) error {
	if strings.Contains(component, "..") {
		return fmt.Errorf("path component contains '..': %q", component)
	}
	if strings.Contains(component, "/") {
		return fmt.Errorf("path component contains '/': %q", component)
	}
	return nil
}

// BuildPath constructs the standard secret file path for a provider instance.
// Pattern: /etc/secrets/{provider}/{instance}/{key}
func BuildPath(provider, instance, key string) string {
	return fmt.Sprintf("/etc/secrets/%s/%s/%s", provider, instance, key)
}

// ResolveOrInfer resolves a secret from an explicit path, or infers the path if empty.
// If customKey is provided, it overrides the default key name.
func ResolveOrInfer(explicitPath, provider, instance, defaultKey, customKey string, log *zap.Logger) (string, error) {
	path := explicitPath
	if path == "" {
		key := defaultKey
		if customKey != "" {
			key = customKey
		}
		if err := sanitizePath(instance); err != nil {
			return "", fmt.Errorf("invalid secret instance: %w", err)
		}
		path = BuildPath(provider, instance, key)
	}
	return Resolve(path, log)
}
