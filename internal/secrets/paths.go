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

// InferPath returns the explicit secret path, or the inferred standard path
// when explicitPath is empty. If customKey is provided, it overrides the
// default key name. Unlike ResolveOrInfer it does not read the file, so callers
// that need to re-read the secret at runtime can keep the resolved path.
func InferPath(explicitPath, provider, instance, defaultKey, customKey string) (string, error) {
	if explicitPath != "" {
		return explicitPath, nil
	}
	key := defaultKey
	if customKey != "" {
		key = customKey
	}
	if err := sanitizePath(instance); err != nil {
		return "", fmt.Errorf("invalid secret instance: %w", err)
	}
	return BuildPath(provider, instance, key), nil
}

// ResolveOrInfer resolves a secret from an explicit path, or infers the path if empty.
// If customKey is provided, it overrides the default key name.
func ResolveOrInfer(explicitPath, provider, instance, defaultKey, customKey string, log *zap.Logger) (string, error) {
	path, err := InferPath(explicitPath, provider, instance, defaultKey, customKey)
	if err != nil {
		return "", err
	}
	return Resolve(path, log)
}
