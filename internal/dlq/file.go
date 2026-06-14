package dlq

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/event"
)

// DefaultMaxSizeBytes bounds the DLQ file before oldest entries are dropped.
const DefaultMaxSizeBytes = 10 * 1024 * 1024 // 10MB

var defaultLogger = zap.NewNop()

// FileQueue persists dead events as JSON lines in a single file.
// Suitable for the relay's low dead-event volume; the whole file is
// rewritten on Remove and on size-bound compaction.
type FileQueue struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
}

// NewFileQueue creates (or reopens) a JSONL-backed queue at path.
func NewFileQueue(path string, maxBytes int64) (*FileQueue, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxSizeBytes
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("dlq: create dir: %w", err)
	}
	// Touch the file so misconfigured paths fail at startup, not at first error.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- path from validated config
	if err != nil {
		return nil, fmt.Errorf("dlq: open file: %w", err)
	}
	_ = f.Close()
	return &FileQueue{path: path, maxBytes: maxBytes}, nil
}

// Enqueue appends or replaces the entry for the envelope's CloudEvent ID.
func (q *FileQueue) Enqueue(_ context.Context, env *event.Envelope, cause error) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	entries, err := q.readAll()
	if err != nil {
		return err
	}

	entry := DeadEvent{
		ID:       env.CloudEventID,
		FailedAt: time.Now().UTC(),
		Cause:    cause.Error(),
		Envelope: env,
	}

	replaced := false
	for i, e := range entries {
		if e.ID == entry.ID {
			entry.RetryCount = e.RetryCount + 1
			entries[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, entry)
	}

	return q.writeAll(entries)
}

// List returns up to limit entries, oldest first.
func (q *FileQueue) List(_ context.Context, limit int) ([]DeadEvent, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	entries, err := q.readAll()
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// Remove deletes the entry with the given CloudEvent ID.
func (q *FileQueue) Remove(_ context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	entries, err := q.readAll()
	if err != nil {
		return err
	}
	out := entries[:0]
	for _, e := range entries {
		if e.ID != id {
			out = append(out, e)
		}
	}
	return q.writeAll(out)
}

// Size reports the number of stored entries.
func (q *FileQueue) Size(_ context.Context) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	entries, err := q.readAll()
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// Close is a no-op; the file is opened per operation.
func (q *FileQueue) Close() error { return nil }

func (q *FileQueue) readAll() ([]DeadEvent, error) {
	f, err := os.Open(q.path) // #nosec G304 -- path from validated config
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("dlq: open: %w", err)
	}
	defer func() { _ = f.Close() }()

	var entries []DeadEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e DeadEvent
		if err := json.Unmarshal(line, &e); err != nil {
			defaultLogger.Warn("dlq: skipping corrupt line", zap.Error(err))
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("dlq: read: %w", err)
	}
	return entries, nil
}

// writeAll rewrites the file atomically, dropping oldest entries while the
// encoded size exceeds the configured bound.
func (q *FileQueue) writeAll(entries []DeadEvent) error {
	encoded := make([][]byte, len(entries))
	var total int64
	for i, e := range entries {
		raw, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("dlq: marshal: %w", err)
		}
		encoded[i] = raw
		total += int64(len(raw)) + 1
	}
	for len(encoded) > 0 && total > q.maxBytes {
		total -= int64(len(encoded[0])) + 1
		encoded = encoded[1:]
	}

	tmp := q.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) // #nosec G304 -- path from validated config
	if err != nil {
		return fmt.Errorf("dlq: open tmp: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, raw := range encoded {
		if _, err := w.Write(raw); err != nil {
			_ = f.Close()
			return fmt.Errorf("dlq: write: %w", err)
		}
		if err := w.WriteByte('\n'); err != nil {
			_ = f.Close()
			return fmt.Errorf("dlq: write: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return fmt.Errorf("dlq: flush: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("dlq: close tmp: %w", err)
	}
	if err := os.Rename(tmp, q.path); err != nil {
		return fmt.Errorf("dlq: rename: %w", err)
	}
	return nil
}
