package secrets

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
)

// FileReader abstracts filesystem access for testing.
type FileReader interface {
	ReadFile(path string) ([]byte, error)
}

// OSFileReader reads from the OS filesystem.
type OSFileReader struct{}

// ReadFile implements FileReader for the OS filesystem.
func (OSFileReader) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path) //nolint:gosec // path is operator-configured
}

// DefaultReader is the default filesystem reader (OS filesystem).
var DefaultReader FileReader = OSFileReader{}

// Resolve reads a secret from the specified file path with logging.
// Returns the trimmed file content or an error if the file doesn't exist or is unreadable.
func Resolve(filePath string, log *zap.Logger) (string, error) {
	return ResolveWithReader(filePath, DefaultReader, log)
}

// ResolveWithReader reads a secret from the specified file path using a custom FileReader.
// This variant exists for testing with mock filesystems.
func ResolveWithReader(filePath string, reader FileReader, log *zap.Logger) (string, error) {
	if filePath == "" {
		if log != nil {
			log.Error("secret_resolution_failed_empty_path")
		}
		return "", fmt.Errorf("secret file path is empty")
	}

	if log != nil {
		log.Debug("resolving_secret")
	}

	data, err := reader.ReadFile(filePath)
	if err != nil {
		if log != nil {
			log.Error("secret_read_failed",
				zap.Error(err),
			)
		}
		return "", fmt.Errorf("read secret from %s: %w", filePath, err)
	}

	// Trim trailing whitespace (common with Kubernetes secret mounts)
	trimmed := strings.TrimSpace(string(data))

	if log != nil {
		log.Debug("secret_resolved")
	}

	return trimmed, nil
}

// CheckAccess verifies that a secret file is readable.
// Returns nil if the file exists and is readable, error otherwise.
func CheckAccess(filePath string) error {
	return CheckAccessWithReader(filePath, DefaultReader)
}

// CheckAccessWithReader verifies secret file accessibility with a custom FileReader.
func CheckAccessWithReader(filePath string, reader FileReader) error {
	if filePath == "" {
		return nil // Empty path is valid (means secret not configured)
	}

	_, err := reader.ReadFile(filePath)
	return err
}
