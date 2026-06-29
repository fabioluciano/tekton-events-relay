package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
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
	Filtered     bool     `json:"filtered,omitempty"`
	Overridden   bool     `json:"overridden,omitempty"`
}

type replayRequest struct {
	Filter   dlq.ReplayFilter `json:"filter"`
	Override *replayOverride  `json:"override,omitempty"`
}

// replayOverride holds optional fields to merge onto each dead event before
// replay. Payload fields are merged onto domain.Event; Timestamp replaces
// CloudEventID (new dedup identity) and FailedAt.
type replayOverride struct {
	Payload   map[string]any `json:"payload,omitempty"`
	Timestamp string         `json:"timestamp,omitempty"`
}

// applyOverride merges the override onto a dead event's envelope. It mutates a
// copy so the DLQ entry is never modified. Returns the overridden DeadEvent.
// The caller must have already validated the timestamp format.
func applyOverride(entry dlq.DeadEvent, o replayOverride) (dlq.DeadEvent, error) {
	out := entry

	if o.Payload != nil && out.Envelope != nil {
		envCopy := *out.Envelope
		reportCopy := envCopy.Report

		base, err := json.Marshal(reportCopy)
		if err != nil {
			return out, fmt.Errorf("marshal report: %w", err)
		}
		var merged map[string]any
		if err := json.Unmarshal(base, &merged); err != nil {
			return out, fmt.Errorf("unmarshal report base: %w", err)
		}
		for k, v := range o.Payload {
			merged[k] = v
		}
		mergedJSON, err := json.Marshal(merged)
		if err != nil {
			return out, fmt.Errorf("marshal merged: %w", err)
		}
		if err := json.Unmarshal(mergedJSON, &reportCopy); err != nil {
			return out, fmt.Errorf("unmarshal merged report: %w", err)
		}
		envCopy.Report = reportCopy
		out.Envelope = &envCopy
	}

	if o.Timestamp != "" {
		ts, err := time.Parse(time.RFC3339, o.Timestamp)
		if err != nil {
			return out, fmt.Errorf("parse timestamp: %w", err)
		}
		out.ID = fmt.Sprintf("override-%d", ts.UnixNano())
		out.FailedAt = ts
		if out.Envelope != nil {
			envCopy := *out.Envelope
			envCopy.CloudEventID = out.ID
			out.Envelope = &envCopy
		}
	}

	return out, nil
}

// validateOverride checks override fields for well-formedness before processing.
func validateOverride(o *replayOverride) error {
	if o == nil {
		return nil
	}
	if o.Timestamp != "" {
		if _, err := time.Parse(time.RFC3339, o.Timestamp); err != nil {
			return fmt.Errorf("invalid timestamp %q: %w", o.Timestamp, err)
		}
	}
	return nil
}

// dlqReplayHandler serves POST /api/v1/dlq/replay. Each stored event is
// re-injected into the processing chain; successfully replayed events are
// removed from the queue, failures stay (with their retry count bumped on
// the next permanent failure). An optional JSON body narrows replay to
// matching events via filter fields (provider, state, after).
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

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		var filter *dlq.ReplayFilter
		var override *replayOverride
		if len(bytes.TrimSpace(body)) > 0 {
			var req replayRequest
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			filter = &req.Filter
			override = req.Override
		}

		if err := validateOverride(override); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		entries, err := collectReplayEntries(r.Context(), queue, filter)
		if err != nil {
			log.Error("dlq list failed", zap.Error(err))
			http.Error(w, "dlq unavailable", http.StatusInternalServerError)
			return
		}

		var result dlqReplayResult
		if filter != nil {
			result.Filtered = true
		}
		if override != nil {
			result.Overridden = true
		}
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

				replayEntry := entry
				if override != nil {
					var oerr error
					replayEntry, oerr = applyOverride(entry, *override)
					if oerr != nil {
						mu.Lock()
						result.Failed++
						result.FailedID = append(result.FailedID, entry.ID)
						mu.Unlock()
						log.Warn("dlq override failed",
							zap.String("ce_id", entry.ID),
							zap.Error(oerr))
						return
					}
				}

				if err := chain.Handle(r.Context(), replayEntry.Envelope); err != nil {
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

func collectReplayEntries(ctx context.Context, queue dlq.Queue, filter *dlq.ReplayFilter) ([]dlq.DeadEvent, error) {
	if filter == nil {
		return queue.List(ctx, 0)
	}

	var entries []dlq.DeadEvent
	if err := queue.Scan(ctx, func(e dlq.DeadEvent) error {
		if filter.Matches(e) {
			entries = append(entries, e)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []dlq.DeadEvent{}
	}
	return entries, nil
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

const dlqAnalyticsTopCausesLimit = 10

type causeCount struct {
	Cause string `json:"cause"`
	Count int    `json:"count"`
}

type dlqAnalyticsResponse struct {
	TopCauses  []causeCount   `json:"top_causes"`
	ByProvider map[string]int `json:"by_provider"`
	ByState    map[string]int `json:"by_state"`
	AgeBuckets map[string]int `json:"age_buckets"`
	Total      int            `json:"total"`
}

func dlqAnalyticsHandler(queue dlq.Queue, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		now := time.Now().UTC()
		causes := make(map[string]int)
		byProvider := make(map[string]int)
		byState := make(map[string]int)
		ageBuckets := map[string]int{"lt_1h": 0, "lt_24h": 0, "gt_24h": 0}
		total := 0

		if err := queue.Scan(r.Context(), func(e dlq.DeadEvent) error {
			total++
			causes[e.Cause]++
			if e.Envelope != nil {
				if p := e.Envelope.Report.Provider; p != "" {
					byProvider[p]++
				}
				if s := string(e.Envelope.Report.State); s != "" {
					byState[s]++
				}
			}
			age := now.Sub(e.FailedAt)
			switch {
			case age < time.Hour:
				ageBuckets["lt_1h"]++
			case age < 24*time.Hour:
				ageBuckets["lt_24h"]++
			default:
				ageBuckets["gt_24h"]++
			}
			return nil
		}); err != nil {
			log.Error("dlq analytics scan failed", zap.Error(err))
			http.Error(w, `{"error":"dlq scan failed"}`, http.StatusInternalServerError)
			return
		}

		topCauses := make([]causeCount, 0, len(causes))
		for c, n := range causes {
			topCauses = append(topCauses, causeCount{Cause: c, Count: n})
		}
		sort.Slice(topCauses, func(i, j int) bool {
			return topCauses[i].Count > topCauses[j].Count
		})
		if len(topCauses) > dlqAnalyticsTopCausesLimit {
			topCauses = topCauses[:dlqAnalyticsTopCausesLimit]
		}

		resp := dlqAnalyticsResponse{
			TopCauses:  topCauses,
			ByProvider: byProvider,
			ByState:    byState,
			AgeBuckets: ageBuckets,
			Total:      total,
		}
		if resp.TopCauses == nil {
			resp.TopCauses = []causeCount{}
		}
		if resp.ByProvider == nil {
			resp.ByProvider = map[string]int{}
		}
		if resp.ByState == nil {
			resp.ByState = map[string]int{}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Error("dlq analytics encode failed", zap.Error(err))
		}
	}
}
