package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/dlq"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
)

const (
	dlqDefaultListLimit  = 100
	dlqMaxListLimit      = 100
	dlqReplayConcurrency = 10
	dlqReplayRPS         = 10
	dlqReplayBurst       = 20
)

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
				if limit > dlqMaxListLimit {
					limit = dlqMaxListLimit
				}
			}
		}

		offset := 0
		if raw := r.URL.Query().Get("offset"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
				offset = v
			}
		}

		// Request enough items to support offset+limit pagination.
		// List currently reads all entries into memory, so this is
		// equivalent to requesting the full set and slicing.
		fetchCount := limit + offset
		if fetchCount < 1 {
			fetchCount = 1
		}
		entries, err := queue.List(r.Context(), fetchCount)
		if err != nil {
			log.Error("dlq list failed", zap.Error(err))
			http.Error(w, "dlq unavailable", http.StatusInternalServerError)
			return
		}

		// Fetch the total count independently so pagination metadata is
		// correct even when List truncates entries at (limit + offset).
		total := len(entries)
		if totalCount, err := queue.Size(r.Context()); err == nil {
			total = totalCount
		}
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}
		page := entries[offset:end]
		if page == nil {
			page = []dlq.DeadEvent{}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"count":  len(page),
			"events": page,
			"total":  total,
			"offset": offset,
			"limit":  limit,
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

		var replayStart time.Time
		if collectors != nil && collectors.DlqReplayTotal != nil {
			replayStart = time.Now()
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

		recordDlqReplayMetrics(collectors, replayStart, result.Failed, result.RemoveFailed)
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

func recordDlqReplayMetrics(collectors *metrics.Collectors, start time.Time, failed, removeFailed int) {
	if collectors == nil || collectors.DlqReplayTotal == nil {
		return
	}
	status := "success"
	if failed > 0 || removeFailed > 0 {
		status = "error"
	}
	collectors.DlqReplayTotal.With(prometheus.Labels{"status": status}).Inc()
	collectors.DlqReplayDuration.WithLabelValues().Observe(time.Since(start).Seconds())
}

func updateDLQSizeGauge(ctx context.Context, queue dlq.Queue, collectors *metrics.Collectors) {
	if collectors == nil {
		return
	}
	if size, err := queue.Size(ctx); err == nil {
		collectors.DLQSize.Set(float64(size))
	}
}
