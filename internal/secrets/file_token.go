package secrets

import "context"

// FileTokenSource resolves a token from a mounted secret file on every call.
//
// Kubernetes updates files mounted from a Secret in place when the Secret is
// rotated (projected/volume mounts, not subPath), so re-reading the file at
// request time picks up the new value without restarting the pod. The read is
// cheap because mounted secrets live on an in-memory tmpfs.
//
// It implements the token-refresher contract used by the SCM and notifier
// clients: Token(ctx) (string, error).
type FileTokenSource struct {
	path   string
	reader FileReader
}

// NewFileTokenSource creates a FileTokenSource that re-reads path on each call.
func NewFileTokenSource(path string) *FileTokenSource {
	return &FileTokenSource{path: path, reader: DefaultReader}
}

// Token reads and returns the current trimmed token from the file.
func (f *FileTokenSource) Token(_ context.Context) (string, error) {
	return ResolveWithReader(f.path, f.reader, nil)
}
