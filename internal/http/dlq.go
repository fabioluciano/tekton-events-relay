package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/dlq"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
)

const dlqDefaultListLimit = 100

// dlqListHandler serves GET /api/v1/dlq.
func dlqListHandler(queue dlq.Queue, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		limit := dlqDefaultListLimit
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				limit = v
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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count":  len(entries),
			"events": entries,
		})
	}
}

// dlqReplayResult summarizes a replay request.
type dlqReplayResult struct {
	Replayed int      `json:"replayed"`
	Failed   int      `json:"failed"`
	FailedID []string `json:"failed_ids,omitempty"`
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
		for _, entry := range entries {
			if entry.Envelope == nil {
				_ = queue.Remove(r.Context(), entry.ID)
				continue
			}
			if err := chain.Handle(r.Context(), entry.Envelope); err != nil {
				result.Failed++
				result.FailedID = append(result.FailedID, entry.ID)
				log.Warn("dlq replay failed",
					zap.String("ce_id", entry.ID),
					zap.Error(err))
				continue
			}
			if err := queue.Remove(r.Context(), entry.ID); err != nil {
				log.Warn("dlq remove after replay failed",
					zap.String("ce_id", entry.ID),
					zap.Error(err))
			}
			result.Replayed++
		}

		updateDLQSizeGauge(r.Context(), queue, collectors)

		log.Info("dlq replay finished",
			zap.Int("replayed", result.Replayed),
			zap.Int("failed", result.Failed))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
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
