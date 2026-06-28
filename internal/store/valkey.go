package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
)

const defaultKeyPrefix = "tekton-events-relay"

// flushScript atomically reads and deletes a run hash.
var flushScript = redis.NewScript(`
local v = redis.call('HGETALL', KEYS[1])
redis.call('DEL', KEYS[1])
return v
`)

// valkeyStore backs dedupe and accumulation with any RESP-compatible
// server (Valkey, KeyDB, ...). State is shared by all relay replicas.
type valkeyStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

func newValkeyStore(cfg config.StoreConfig, opts Options) (*valkeyStore, error) {
	var password string
	if cfg.Valkey.PasswordFile != "" {
		p, err := secrets.Resolve(cfg.Valkey.PasswordFile, opts.Log)
		if err != nil {
			return nil, fmt.Errorf("store.valkey: resolve password: %w", err)
		}
		password = p
	}

	prefix := cfg.Valkey.KeyPrefix
	if prefix == "" {
		prefix = defaultKeyPrefix
	}

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Valkey.Address,
		Password: password,
		DB:       cfg.Valkey.DB,
	})

	// Connectivity is verified at startup so misconfiguration fails fast;
	// runtime failures fail open at the call sites.
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		opts.Log.Warn("store.valkey: server unreachable at startup, continuing fail-open",
			zap.String("address", cfg.Valkey.Address), zap.Error(err))
	}

	return &valkeyStore{client: client, prefix: prefix, ttl: cfg.TTL}, nil
}

func (s *valkeyStore) Dedupe() DedupeStore  { return s }
func (s *valkeyStore) RunBuffer() RunBuffer { return s }
func (s *valkeyStore) Backend() string      { return BackendValkey }
func (s *valkeyStore) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.client.Ping(pingCtx).Err()
}
func (s *valkeyStore) Close() error { return s.client.Close() }

// FirstSeen records id with SET NX EX; the reply reports whether it was new.
func (s *valkeyStore) FirstSeen(ctx context.Context, id string) (bool, error) {
	key := s.prefix + ":dedupe:" + id
	ok, err := s.client.SetNX(ctx, key, "", s.ttl).Result()
	if err != nil {
		return false, fmt.Errorf("valkey SETNX: %w", err)
	}
	return ok, nil
}

func (s *valkeyStore) runKey(uid string) string {
	return s.prefix + ":run:" + uid
}

// Add stores the JSON-encoded event in the run hash and refreshes its TTL.
func (s *valkeyStore) Add(ctx context.Context, uid, task string, ev *domain.Event) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	key := s.runKey(uid)
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key, task, raw)
	pipe.Expire(ctx, key, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("valkey HSET: %w", err)
	}
	return nil
}

// Flush atomically retrieves and deletes the run hash via a Lua script.
func (s *valkeyStore) Flush(ctx context.Context, uid string) (map[string]*domain.Event, bool, error) {
	res, err := flushScript.Run(ctx, s.client, []string{s.runKey(uid)}).Result()
	if err != nil {
		return nil, false, fmt.Errorf("valkey flush: %w", err)
	}

	// HGETALL via EVAL returns a flat [field, value, field, value, ...] array.
	flat, ok := res.([]any)
	if !ok || len(flat) == 0 {
		return nil, false, nil
	}

	out := make(map[string]*domain.Event, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		field, _ := flat[i].(string)
		value, _ := flat[i+1].(string)
		var ev domain.Event
		if err := json.Unmarshal([]byte(value), &ev); err != nil {
			return nil, false, fmt.Errorf("unmarshal event for task %q: %w", field, err)
		}
		out[field] = &ev
	}
	return out, true, nil
}
