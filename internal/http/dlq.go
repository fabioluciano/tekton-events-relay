package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/dlq"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
)

const (
	dlqDefaultListLimit  = 100
	dlqReplayConcurrency = 10
)

// dlqListHandler serves GET /api/v1/dlq.
func dlqListHandler(queue dlq.Queue, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		limit := dlqDefaultListLimit
		const dlqMaxListLimit = 1000
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				limit = v
				if limit > dlqMaxListLimit {
					limit = dlqMaxListLimit
				}
			}
		}

		entries, err := queue.List(r.Context(), limit)
		if err != nil {
			log.Error("dlq list failed", zap.Error(err))
			http.Error(w, "dlq unavailable", http.StatusInternalServerError)
			return
		}
		if entries == nil {
			entries = []dlq.DeadEvent{}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"count":  len(entries),
			"events": entries,
		}); err != nil {
			log.Error("dlq list encode failed", zap.Error(err))
		}
	}
}

// dlqReplayResult summarizes a replay request.
type dlqReplayResult struct {
	Replayed     int      `json:"replayed"`
	Failed       int      `json:"failed"`
	FailedID     []string `json:"failed_ids,omitempty"`
	RemoveFailed int      `json:"remove_failed,omitempty"`
}

// dlqReplayHandler serves POST /api/v1/dlq/replay. Each stored event is
// re-injected into the processing chain; successfully replayed events are
// removed from the queue, failures stay (with their retry count bumped on
// the next permanent failure).
func dlqReplayHandler(queue dlq.Queue, chain pipeline.Handler, collectors *metrics.Collectors, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		entries, err := queue.List(r.Context(), 0)
		if err != nil {
			log.Error("dlq list failed", zap.Error(err))
			http.Error(w, "dlq unavailable", http.StatusInternalServerError)
			return
		}

		var result dlqReplayResult
		var mu sync.Mutex
		sem := make(chan struct{}, dlqReplayConcurrency)
		var wg sync.WaitGroup

		for _, entry := range entries {
			if entry.Envelope == nil {
				_ = queue.Remove(r.Context(), entry.ID)
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(entry dlq.DeadEvent) {
				defer wg.Done()
				defer func() { <-sem }()

				if err := chain.Handle(r.Context(), entry.Envelope); err != nil {
					mu.Lock()
					result.Failed++
					result.FailedID = append(result.FailedID, entry.ID)
					mu.Unlock()
					log.Warn("dlq replay failed",
						zap.String("ce_id", entry.ID),
						zap.Error(err))
					return
				}
				if err := queue.Remove(r.Context(), entry.ID); err != nil {
					mu.Lock()
					result.RemoveFailed++
					mu.Unlock()
					log.Error("dlq remove after replay failed",
						zap.String("ce_id", entry.ID),
						zap.Error(err))
					return
				}
				mu.Lock()
				result.Replayed++
				mu.Unlock()
			}(entry)
		}
		wg.Wait()

		updateDLQSizeGauge(r.Context(), queue, collectors)

		log.Info("dlq replay finished",
			zap.Int("replayed", result.Replayed),
			zap.Int("failed", result.Failed),
			zap.Int("remove_failed", result.RemoveFailed))

		if result.RemoveFailed > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "replay succeeded but failed to remove DLQ entry",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Error("dlq replay encode failed", zap.Error(err))
		}
	}
}

func updateDLQSizeGauge(ctx context.Context, queue dlq.Queue, collectors *metrics.Collectors) {
	if collectors == nil {
		return
	}
	if size, err := queue.Size(ctx); err == nil {
		collectors.DLQSize.Set(float64(size))
	}
}
