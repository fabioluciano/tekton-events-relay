package secrets

import (
	"errors"
	"strings"
	"testing"
)

// mockFileReader simulates filesystem for testing.
type mockFileReader struct {
	files map[string]string
	err   error
}

func (m *mockFileReader) ReadFile(path string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	content, ok := m.files[path]
	if !ok {
		return nil, errors.New("file not found")
	}
	return []byte(content), nil
}

func TestResolve_Success(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/tmp/test-secret": "my-secret-token", //nolint:gosec // test credential value
		},
	}

	result, err := ResolveWithReader("/tmp/test-secret", reader, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "my-secret-token" {
		t.Errorf("expected 'my-secret-token', got '%s'", result)
	}
}

func TestResolve_TrimsWhitespace(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{ //nolint:gosec // test credential path
			"/tmp/test-secret": "my-token\n\n  ",
		},
	}

	result, err := ResolveWithReader("/tmp/test-secret", reader, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "my-token" {
		t.Errorf("expected 'my-token', got '%s'", result)
	}
}

func TestResolve_EmptyPath(t *testing.T) {
	reader := &mockFileReader{files: map[string]string{}}

	_, err := ResolveWithReader("", reader, nil)
	if err == nil {
		t.Error("expected error for empty path, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error mentioning 'empty', got: %v", err)
	}
}

func TestResolve_FileNotFound(t *testing.T) {
	reader := &mockFileReader{files: map[string]string{}}

	_, err := ResolveWithReader("/nonexistent", reader, nil)
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestResolve_PermissionDenied(t *testing.T) {
	reader := &mockFileReader{
		err: errors.New("permission denied"),
	}

	_, err := ResolveWithReader("/restricted", reader, nil)
	if err == nil {
		t.Error("expected permission error, got nil")
	}
}

func TestCheckAccess_Exists(t *testing.T) {
	reader := &mockFileReader{
		files: map[string]string{
			"/tmp/test": "content",
		},
	}

	err := CheckAccessWithReader("/tmp/test", reader)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestCheckAccess_EmptyPath(t *testing.T) {
	reader := &mockFileReader{files: map[string]string{}}

	err := CheckAccessWithReader("", reader)
	if err != nil {
		t.Errorf("expected no error for empty path, got: %v", err)
	}
}

func TestCheckAccess_NotFound(t *testing.T) {
	reader := &mockFileReader{files: map[string]string{}}

	err := CheckAccessWithReader("/nonexistent", reader)
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}
