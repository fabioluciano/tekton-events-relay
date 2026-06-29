package dlq

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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

	retentionDays int
	cancel        context.CancelFunc
	done          chan struct{}
}

// NewFileQueue creates (or reopens) a JSONL-backed queue at path.
// If retentionDays > 0, a background goroutine runs daily to remove
// entries older than retentionDays. The goroutine stops when Close is called.
func NewFileQueue(path string, maxBytes int64, retentionDays int) (*FileQueue, error) {
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

	ctx, cancel := context.WithCancel(context.Background())
	q := &FileQueue{
		path:          path,
		maxBytes:      maxBytes,
		retentionDays: retentionDays,
		cancel:        cancel,
		done:          make(chan struct{}),
	}

	if retentionDays > 0 {
		go q.runRetention(ctx)
	} else {
		close(q.done)
	}

	return q, nil
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

// Close stops the retention goroutine (if running) and waits for it to finish.
func (q *FileQueue) Close() error {
	if q.cancel != nil {
		q.cancel()
	}
	if q.done != nil {
		<-q.done
	}
	return nil
}

// Scan iterates the JSONL file line-by-line without loading the full file.
// It calls fn for each valid DeadEvent; iteration stops when fn returns a
// non-nil error. ErrStopScan is the sentinel to stop early without error.
func (q *FileQueue) Scan(_ context.Context, fn func(DeadEvent) error) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	f, err := os.Open(q.path) // #nosec G304 -- path from validated config
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("dlq: open: %w", err)
	}
	defer func() { _ = f.Close() }()

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
		if err := fn(e); err != nil {
			if errors.Is(err, ErrStopScan) {
				return nil
			}
			return err
		}
	}
	return scanner.Err()
}

func (q *FileQueue) runRetention(ctx context.Context) {
	defer close(q.done)

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			q.purgeExpired()
		}
	}
}

func (q *FileQueue) purgeExpired() {
	q.mu.Lock()
	defer q.mu.Unlock()

	entries, err := q.readAll()
	if err != nil {
		defaultLogger.Warn("dlq: retention read failed", zap.Error(err))
		return
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -q.retentionDays)
	var kept []DeadEvent
	for _, e := range entries {
		if e.FailedAt.Before(cutoff) {
			continue
		}
		kept = append(kept, e)
	}

	removed := len(entries) - len(kept)
	if removed == 0 {
		return
	}

	if err := q.writeAll(kept); err != nil {
		defaultLogger.Warn("dlq: retention write failed", zap.Error(err))
		return
	}
	defaultLogger.Info("dlq: retention cleanup", zap.Int("removed", removed), zap.Int("kept", len(kept)))
}

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
	// Sync data to disk before close so the rename targets durable bytes.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("dlq: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("dlq: close tmp: %w", err)
	}
	if err := os.Rename(tmp, q.path); err != nil {
		return fmt.Errorf("dlq: rename: %w", err)
	}
	// Best-effort parent-directory sync so the directory entry is durable.
	// On platforms/filesystems that do not support fsync on directories
	// (e.g. some network mounts), this is a documented no-op fallback.
	syncParentDir(filepath.Dir(q.path))
	return nil
}

// syncParentDir opens the directory and calls Sync to flush the directory
// entry created by Rename. Errors are silently ignored — this is a
// best-effort durability improvement, not a correctness requirement.
func syncParentDir(dir string) {
	df, err := os.Open(dir) // #nosec G304 -- parent of configured path
	if err != nil {
		return
	}
	defer func() { _ = df.Close() }()
	_ = df.Sync()
}
