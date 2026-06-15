package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/olric-data/olric"
	olricconfig "github.com/olric-data/olric/config"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	olricStartTimeout  = 30 * time.Second
	olricLockDeadline  = 5 * time.Second
	olricLockTimeout   = 10 * time.Second
	olricDedupeDMap    = "dedupe"
	olricRunBufferDMap = "runbuffer"
)

// olricStore embeds a distributed cache inside the relay pods themselves;
// replicas discover each other via memberlist gossip (no extra deployment).
type olricStore struct {
	db     *olric.Olric
	dedupe olric.DMap
	runs   olric.DMap
	ttl    time.Duration
}

func newOlricStore(cfg config.StoreConfig, opts Options) (*olricStore, error) {
	env := cfg.Olric.Env
	if env == "" {
		env = "lan"
	}
	oc := olricconfig.New(env)
	oc.Logger = log.New(&zapLogWriter{log: opts.Log.Named("olric")}, "", 0)
	if cfg.Olric.BindAddr != "" {
		oc.BindAddr = cfg.Olric.BindAddr
		oc.MemberlistConfig.BindAddr = cfg.Olric.BindAddr
		oc.MemberlistConfig.AdvertiseAddr = cfg.Olric.BindAddr
	}
	if cfg.Olric.BindPort != 0 {
		oc.BindPort = cfg.Olric.BindPort
	}
	if cfg.Olric.MemberlistPort != 0 {
		oc.MemberlistConfig.BindPort = cfg.Olric.MemberlistPort
		oc.MemberlistConfig.AdvertisePort = cfg.Olric.MemberlistPort
	}
	if len(cfg.Olric.Peers) > 0 {
		oc.Peers = cfg.Olric.Peers
	}

	started := make(chan struct{})
	oc.Started = func() { close(started) }

	db, err := olric.New(oc)
	if err != nil {
		return nil, fmt.Errorf("store.olric: configure: %w", err)
	}

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		// Start blocks for the lifetime of the cluster member.
		startErr <- db.Start()
	}()

	select {
	case <-started:
	case err := <-startErr:
		return nil, fmt.Errorf("store.olric: start: %w", err)
	case <-time.After(olricStartTimeout):
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = db.Shutdown(shutdownCtx)
		return nil, fmt.Errorf("store.olric: start timed out after %s", olricStartTimeout)
	}

	client := db.NewEmbeddedClient()
	dedupe, err := client.NewDMap(olricDedupeDMap)
	if err != nil {
		return nil, fmt.Errorf("store.olric: dmap %s: %w", olricDedupeDMap, err)
	}
	runs, err := client.NewDMap(olricRunBufferDMap)
	if err != nil {
		return nil, fmt.Errorf("store.olric: dmap %s: %w", olricRunBufferDMap, err)
	}

	return &olricStore{db: db, dedupe: dedupe, runs: runs, ttl: cfg.TTL}, nil
}

func (s *olricStore) Dedupe() DedupeStore  { return s }
func (s *olricStore) RunBuffer() RunBuffer { return s }
func (s *olricStore) Backend() string      { return BackendOlric }

func (s *olricStore) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.db.Shutdown(ctx)
}

// FirstSeen records id with PUT NX EX; ErrKeyFound means a duplicate.
func (s *olricStore) FirstSeen(ctx context.Context, id string) (bool, error) {
	err := s.dedupe.Put(ctx, id, "", olric.NX(), olric.EX(s.ttl))
	if errors.Is(err, olric.ErrKeyFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("olric put: %w", err)
	}
	return true, nil
}

// Add merges the event into the run's task map under a per-run lock.
func (s *olricStore) Add(ctx context.Context, uid, task string, ev *domain.Event) error {
	lock, err := s.runs.LockWithTimeout(ctx, "lock:"+uid, olricLockTimeout, olricLockDeadline)
	if err != nil {
		return fmt.Errorf("olric lock: %w", err)
	}
	defer func() { _ = lock.Unlock(ctx) }()

	tasks, err := s.getTasks(ctx, uid)
	if err != nil {
		return err
	}
	tasks[task] = ev

	raw, err := json.Marshal(tasks)
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}
	if err := s.runs.Put(ctx, uid, raw, olric.EX(s.ttl)); err != nil {
		return fmt.Errorf("olric put: %w", err)
	}
	return nil
}

// Flush removes and returns the run's task map under a per-run lock.
func (s *olricStore) Flush(ctx context.Context, uid string) (map[string]*domain.Event, bool, error) {
	lock, err := s.runs.LockWithTimeout(ctx, "lock:"+uid, olricLockTimeout, olricLockDeadline)
	if err != nil {
		return nil, false, fmt.Errorf("olric lock: %w", err)
	}
	defer func() { _ = lock.Unlock(ctx) }()

	tasks, err := s.getTasks(ctx, uid)
	if err != nil {
		return nil, false, err
	}
	if len(tasks) == 0 {
		return nil, false, nil
	}
	if _, err := s.runs.Delete(ctx, uid); err != nil {
		return nil, false, fmt.Errorf("olric delete: %w", err)
	}
	return tasks, true, nil
}

func (s *olricStore) getTasks(ctx context.Context, uid string) (map[string]*domain.Event, error) {
	resp, err := s.runs.Get(ctx, uid)
	if errors.Is(err, olric.ErrKeyNotFound) {
		return make(map[string]*domain.Event), nil
	}
	if err != nil {
		return nil, fmt.Errorf("olric get: %w", err)
	}
	raw, err := resp.Byte()
	if err != nil {
		return nil, fmt.Errorf("olric value: %w", err)
	}
	tasks := make(map[string]*domain.Event)
	if err := json.Unmarshal(raw, &tasks); err != nil {
		return nil, fmt.Errorf("unmarshal tasks: %w", err)
	}
	return tasks, nil
}

var olricLogPattern = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} \[(DEBUG|INFO|WARN|ERROR|ERR)\] (.*)$`)

// zapLogWriter adapts olric's std log output to structured zap logging.
type zapLogWriter struct {
	log *zap.Logger
}

func (w *zapLogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSuffix(string(p), "\n")

	matches := olricLogPattern.FindStringSubmatch(msg)
	if len(matches) == 3 {
		level := matches[1]
		text := matches[2]

		switch level {
		case "DEBUG":
			w.log.Debug(text)
		case "INFO":
			w.log.Info(text)
		case "WARN":
			w.log.Warn(text)
		case "ERROR", "ERR":
			w.log.Error(text)
		}
	} else {
		// Fallback for non-standard format
		w.log.Info(msg)
	}

	return len(p), nil
}
