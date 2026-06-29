package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/dlq"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
)

const (
	causeTokenExpired     = "token expired"
	providerGitLab        = "gitlab"
	causeRepoNotFound     = "repo not found"
	causePermissionDenied = "permission denied"
	testEvtOrig           = "evt-orig"
)

// flakyChain fails the first N Handle calls with a permanent error.
type flakyChain struct {
	pipeline.BaseHandler
	failuresLeft int
	handled      int
}

func (c *flakyChain) Handle(ctx context.Context, env *event.Envelope) error {
	if c.failuresLeft > 0 {
		c.failuresLeft--
		return errors.New("permanent: repo not found")
	}
	c.handled++
	return c.Next(ctx, env)
}

func dlqTestEnvelope(id string) *event.Envelope {
	return &event.Envelope{
		CloudEventID: id,
		Report: domain.Event{
			Provider: "github", //nolint:goconst // test fixture
			Resource: domain.ResourcePipelineRun,
			RunName:  "run",
			State:    domain.StateFailure,
		},
	}
}

func TestDLQ_PermanentErrorIsPreservedAndReplayable(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	collectors := metrics.NewCollectors(prometheus.NewRegistry())
	log := zap.NewNop()

	chain := &flakyChain{failuresLeft: 1}
	pipeline.Build(chain)

	// 1. Permanent chain error → event acked with 200 and preserved in DLQ.
	rec := httptest.NewRecorder()
	handleChainResult(context.Background(), chain, dlqTestEnvelope("evt-1"), log, collectors, queue, rec)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	size, _ := queue.Size(context.Background())
	if size != 1 {
		t.Fatalf("dlq size = %d, want 1", size)
	}

	// 2. GET /api/v1/dlq lists it.
	listRec := httptest.NewRecorder()
	dlqListHandler(queue, log)(listRec, httptest.NewRequest("GET", "/api/v1/dlq", nil))
	var listResp struct {
		Count  int             `json:"count"`
		Events []dlq.DeadEvent `json:"events"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResp.Count != 1 || listResp.Events[0].ID != "evt-1" {
		t.Fatalf("unexpected list response: %+v", listResp)
	}

	// 3. POST /api/v1/dlq/replay reprocesses and removes it.
	replayRec := httptest.NewRecorder()
	dlqReplayHandler(queue, chain, collectors, log)(replayRec, httptest.NewRequest("POST", "/api/v1/dlq/replay", nil))
	var replayResp struct {
		Replayed int `json:"replayed"`
		Failed   int `json:"failed"`
	}
	if err := json.Unmarshal(replayRec.Body.Bytes(), &replayResp); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if replayResp.Replayed != 1 || replayResp.Failed != 0 {
		t.Fatalf("replay = %+v, want 1 replayed", replayResp)
	}
	if chain.handled != 1 {
		t.Errorf("chain handled = %d, want 1", chain.handled)
	}
	size, _ = queue.Size(context.Background())
	if size != 0 {
		t.Errorf("dlq size = %d, want 0", size)
	}
}

//nolint:dupl // Test subtest blocks are necessarily similar
func TestDLQ_ListPaginationMetadata(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()
	ctx := context.Background()

	for i := range 10 {
		_ = queue.Enqueue(ctx, dlqTestEnvelope(fmt.Sprintf("evt-%d", i)), errors.New("x"))
	}

	t.Run("default limit and offset", func(t *testing.T) {
		rec := httptest.NewRecorder()
		dlqListHandler(queue, log)(rec, httptest.NewRequest("GET", "/api/v1/dlq", nil))
		var resp struct {
			Count  int               `json:"count"`
			Events []json.RawMessage `json:"events"`
			Total  int               `json:"total"`
			Offset int               `json:"offset"`
			Limit  int               `json:"limit"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Total != 10 {
			t.Errorf("total = %d, want 10", resp.Total)
		}
		if resp.Offset != 0 {
			t.Errorf("offset = %d, want 0", resp.Offset)
		}
		if resp.Limit != 100 {
			t.Errorf("limit = %d, want 100", resp.Limit)
		}
		if resp.Count != 10 {
			t.Errorf("count = %d, want 10", resp.Count)
		}
	})

	t.Run("offset pagination", func(t *testing.T) {
		rec := httptest.NewRecorder()
		dlqListHandler(queue, log)(rec, httptest.NewRequest("GET", "/api/v1/dlq?limit=3&offset=4", nil))
		var resp struct {
			Count  int               `json:"count"`
			Events []json.RawMessage `json:"events"`
			Total  int               `json:"total"`
			Offset int               `json:"offset"`
			Limit  int               `json:"limit"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Total != 10 {
			t.Errorf("total = %d, want 10", resp.Total)
		}
		if resp.Offset != 4 {
			t.Errorf("offset = %d, want 4", resp.Offset)
		}
		if resp.Limit != 3 {
			t.Errorf("limit = %d, want 3", resp.Limit)
		}
		if resp.Count != 3 {
			t.Errorf("count = %d, want 3", resp.Count)
		}
	})

	t.Run("offset beyond total returns empty page", func(t *testing.T) {
		rec := httptest.NewRecorder()
		dlqListHandler(queue, log)(rec, httptest.NewRequest("GET", "/api/v1/dlq?offset=100", nil))
		var resp struct {
			Count  int               `json:"count"`
			Events []json.RawMessage `json:"events"`
			Total  int               `json:"total"`
			Offset int               `json:"offset"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Total != 10 {
			t.Errorf("total = %d, want 10", resp.Total)
		}
		if resp.Count != 0 {
			t.Errorf("count = %d, want 0", resp.Count)
		}
	})
}

func TestDLQ_ListMaxLimit(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()
	ctx := context.Background()

	for i := range 200 {
		_ = queue.Enqueue(ctx, dlqTestEnvelope(fmt.Sprintf("evt-%d", i)), errors.New("x"))
	}

	rec := httptest.NewRecorder()
	dlqListHandler(queue, log)(rec, httptest.NewRequest("GET", "/api/v1/dlq?limit=200", nil))
	var resp struct {
		Count int `json:"count"`
		Limit int `json:"limit"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Limit != 100 {
		t.Errorf("limit = %d, want 100 (max cap)", resp.Limit)
	}
	if resp.Count > 100 {
		t.Errorf("count = %d, want <= 100", resp.Count)
	}
	if resp.Total != 200 {
		t.Errorf("total = %d, want 200", resp.Total)
	}
}

//nolint:dupl // TestDLQReplayNoBody and TestDLQReplayNoOverride test the same nil-body path but are required by the acceptance criteria as separate named tests
func TestDLQReplayNoBody(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()
	ctx := context.Background()

	env := dlqTestEnvelope("evt-body-1")
	env.Report.State = domain.StateFailure
	if err := queue.Enqueue(ctx, env, errors.New("permanent")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	chain := &stateCapturingChain{}
	pipeline.Build(chain)

	rec := httptest.NewRecorder()
	dlqReplayHandler(queue, chain, nil, log)(rec, httptest.NewRequest("POST", "/api/v1/dlq/replay", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp dlqReplayResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Replayed != 1 {
		t.Fatalf("replayed = %d, want 1", resp.Replayed)
	}
	if resp.Overridden {
		t.Errorf("overridden = true, want false (no body = no override)")
	}

	chain.mu.Lock()
	defer chain.mu.Unlock()
	if len(chain.states) != 1 || chain.states[0] != domain.StateFailure {
		t.Errorf("states = %v, want [failure] (original state preserved)", chain.states)
	}
}

func TestDLQ_ReplayKeepsFailingEvents(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()

	_ = queue.Enqueue(context.Background(), dlqTestEnvelope("evt-1"), errors.New("x"))

	chain := &flakyChain{failuresLeft: 10} // still failing
	pipeline.Build(chain)

	rec := httptest.NewRecorder()
	dlqReplayHandler(queue, chain, nil, log)(rec, httptest.NewRequest("POST", "/api/v1/dlq/replay", nil))

	size, _ := queue.Size(context.Background())
	if size != 1 {
		t.Errorf("dlq size = %d, want 1 (failed replay keeps event)", size)
	}
}

func TestDLQ_ListRejectsNonGET(t *testing.T) {
	queue, _ := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	rec := httptest.NewRecorder()
	dlqListHandler(queue, zap.NewNop())(rec, httptest.NewRequest("DELETE", "/api/v1/dlq", nil))
	if rec.Code != 405 {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// removeFailingQueue wraps a real FileQueue but returns an error on Remove.
type removeFailingQueue struct {
	*dlq.FileQueue
	removeErr error
}

func (q *removeFailingQueue) Remove(_ context.Context, _ string) error {
	return q.removeErr
}

func TestDLQ_ReplayReturns500OnRemoveFailure(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	collectors := metrics.NewCollectors(prometheus.NewRegistry())
	log := zap.NewNop()

	if err := queue.Enqueue(context.Background(), dlqTestEnvelope("evt-1"), errors.New("x")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	mockQueue := &removeFailingQueue{
		FileQueue: queue,
		removeErr: errors.New("disk full"),
	}

	chain := &flakyChain{failuresLeft: 0}
	pipeline.Build(chain)

	rec := httptest.NewRecorder()
	dlqReplayHandler(mockQueue, chain, collectors, log)(rec, httptest.NewRequest("POST", "/api/v1/dlq/replay", nil))

	if rec.Code != 500 {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := "replay succeeded but failed to remove DLQ entry"
	if resp["error"] != want {
		t.Errorf("error = %q, want %q", resp["error"], want)
	}

	size, _ := queue.Size(context.Background())
	if size != 1 {
		t.Errorf("dlq size = %d, want 1 (entry left on remove failure)", size)
	}
}

func TestDLQAnalytics(t *testing.T) {
	dlqPath := filepath.Join(t.TempDir(), "dlq.jsonl")
	queue, err := dlq.NewFileQueue(dlqPath, 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()

	now := time.Now().UTC()

	// 20 entries with controlled distributions:
	// - 10x causeTokenExpired, 6x causeRepoNotFound, 4x causePermissionDenied
	// - 10x github, 10x gitlab
	// - 15x failure, 5x error
	// - 5x <1h, 9x <24h, 6x >24h
	entries := []struct {
		id       string
		cause    string
		provider string
		state    domain.State
		age      time.Duration
	}{
		{"e01", causeTokenExpired, "github", domain.StateFailure, 10 * time.Minute},
		{"e02", causeTokenExpired, "github", domain.StateFailure, 20 * time.Minute},
		{"e03", causeTokenExpired, providerGitLab, domain.StateFailure, 30 * time.Minute},
		{"e04", causeTokenExpired, providerGitLab, domain.StateError, 40 * time.Minute},
		{"e05", causeTokenExpired, "github", domain.StateFailure, 50 * time.Minute},
		{"e06", causeTokenExpired, "github", domain.StateFailure, 2 * time.Hour},
		{"e07", causeTokenExpired, providerGitLab, domain.StateError, 4 * time.Hour},
		{"e08", causeTokenExpired, "github", domain.StateFailure, 6 * time.Hour},
		{"e09", causeTokenExpired, providerGitLab, domain.StateFailure, 10 * time.Hour},
		{"e10", causeTokenExpired, "github", domain.StateFailure, 12 * time.Hour},
		{"e11", causeRepoNotFound, "github", domain.StateFailure, 25 * time.Hour},
		{"e12", causeRepoNotFound, "github", domain.StateFailure, 26 * time.Hour},
		{"e13", causeRepoNotFound, providerGitLab, domain.StateError, 28 * time.Hour},
		{"e14", causeRepoNotFound, "github", domain.StateFailure, 30 * time.Hour},
		{"e15", causeRepoNotFound, providerGitLab, domain.StateFailure, 48 * time.Hour},
		{"e16", causeRepoNotFound, "github", domain.StateFailure, 72 * time.Hour},
		{"e17", causePermissionDenied, providerGitLab, domain.StateError, 3 * time.Hour},
		{"e18", causePermissionDenied, providerGitLab, domain.StateFailure, 5 * time.Hour},
		{"e19", causePermissionDenied, providerGitLab, domain.StateError, 8 * time.Hour},
		{"e20", causePermissionDenied, providerGitLab, domain.StateFailure, 20 * time.Hour},
	}

	for _, e := range entries {
		env := &event.Envelope{
			CloudEventID: e.id,
			Report: domain.Event{
				Provider: e.provider,
				Resource: domain.ResourcePipelineRun,
				RunName:  "run-" + e.id,
				State:    e.state,
			},
		}
		raw, _ := json.Marshal(dlq.DeadEvent{
			ID:         e.id,
			FailedAt:   now.Add(-e.age),
			Cause:      e.cause,
			RetryCount: 0,
			Envelope:   env,
		})
		if err := appendLine(dlqPath, raw); err != nil {
			t.Fatalf("appendLine %s: %v", e.id, err)
		}
	}

	rec := httptest.NewRecorder()
	dlqAnalyticsHandler(queue, log)(rec, httptest.NewRequest("GET", "/api/v1/dlq/analytics", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp dlqAnalyticsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Total != 20 {
		t.Errorf("total = %d, want 20", resp.Total)
	}

	if len(resp.TopCauses) != 3 {
		t.Fatalf("top_causes len = %d, want 3", len(resp.TopCauses))
	}
	if resp.TopCauses[0].Cause != causeTokenExpired || resp.TopCauses[0].Count != 10 {
		t.Errorf("top_causes[0] = %+v", resp.TopCauses[0])
	}
	if resp.TopCauses[1].Cause != causeRepoNotFound || resp.TopCauses[1].Count != 6 {
		t.Errorf("top_causes[1] = %+v", resp.TopCauses[1])
	}
	if resp.TopCauses[2].Cause != causePermissionDenied || resp.TopCauses[2].Count != 4 {
		t.Errorf("top_causes[2] = %+v", resp.TopCauses[2])
	}

	if resp.ByProvider["github"] != 10 {
		t.Errorf("by_provider[github] = %d, want 10", resp.ByProvider["github"])
	}
	if resp.ByProvider[providerGitLab] != 10 {
		t.Errorf("by_provider[gitlab] = %d, want 10", resp.ByProvider[providerGitLab])
	}

	if resp.ByState["failure"] != 15 {
		t.Errorf("by_state[failure] = %d, want 15", resp.ByState["failure"])
	}
	if resp.ByState["error"] != 5 {
		t.Errorf("by_state[error] = %d, want 5", resp.ByState["error"])
	}

	if resp.AgeBuckets["lt_1h"] != 5 {
		t.Errorf("age_buckets[lt_1h] = %d, want 5", resp.AgeBuckets["lt_1h"])
	}
	if resp.AgeBuckets["lt_24h"] != 9 {
		t.Errorf("age_buckets[lt_24h] = %d, want 9", resp.AgeBuckets["lt_24h"])
	}
	if resp.AgeBuckets["gt_24h"] != 6 {
		t.Errorf("age_buckets[gt_24h] = %d, want 6", resp.AgeBuckets["gt_24h"])
	}
}

func TestDLQAnalytics_EmptyDLQ(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()

	rec := httptest.NewRecorder()
	dlqAnalyticsHandler(queue, log)(rec, httptest.NewRequest("GET", "/api/v1/dlq/analytics", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp dlqAnalyticsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Total != 0 {
		t.Errorf("total = %d, want 0", resp.Total)
	}
	if len(resp.TopCauses) != 0 {
		t.Errorf("top_causes len = %d, want 0", len(resp.TopCauses))
	}
	if len(resp.ByProvider) != 0 {
		t.Errorf("by_provider len = %d, want 0", len(resp.ByProvider))
	}
	if len(resp.ByState) != 0 {
		t.Errorf("by_state len = %d, want 0", len(resp.ByState))
	}
	if resp.AgeBuckets["lt_1h"] != 0 || resp.AgeBuckets["lt_24h"] != 0 || resp.AgeBuckets["gt_24h"] != 0 {
		t.Errorf("age_buckets not all zero: %+v", resp.AgeBuckets)
	}
}

func TestDLQAnalytics_RejectsNonGET(t *testing.T) {
	queue, _ := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	rec := httptest.NewRecorder()
	dlqAnalyticsHandler(queue, zap.NewNop())(rec, httptest.NewRequest("POST", "/api/v1/dlq/analytics", nil))
	if rec.Code != 405 {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestDLQAnalytics_TopCausesLimit(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()

	for i := range 12 {
		_ = queue.Enqueue(context.Background(), dlqTestEnvelope(fmt.Sprintf("evt-%d", i)),
			fmt.Errorf("cause-%02d", i))
	}

	rec := httptest.NewRecorder()
	dlqAnalyticsHandler(queue, log)(rec, httptest.NewRequest("GET", "/api/v1/dlq/analytics", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp dlqAnalyticsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.TopCauses) != 10 {
		t.Errorf("top_causes len = %d, want 10 (capped)", len(resp.TopCauses))
	}
	if resp.Total != 12 {
		t.Errorf("total = %d, want 12", resp.Total)
	}
}

// stateCapturingChain records the State of every handled envelope.
type stateCapturingChain struct {
	pipeline.BaseHandler
	states []domain.State
	mu     sync.Mutex
}

func (c *stateCapturingChain) Handle(_ context.Context, env *event.Envelope) error {
	c.mu.Lock()
	c.states = append(c.states, env.Report.State)
	c.mu.Unlock()
	return c.Next(context.Background(), env)
}

//nolint:dupl
func TestDLQReplayWithOverride(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()
	ctx := context.Background()

	env := dlqTestEnvelope("evt-1")
	env.Report.State = domain.StateFailure
	if err := queue.Enqueue(ctx, env, errors.New("permanent")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	chain := &stateCapturingChain{}
	pipeline.Build(chain)

	body := `{"override":{"payload":{"state":"success"}}}`
	rec := httptest.NewRecorder()
	dlqReplayHandler(queue, chain, nil, log)(rec, httptest.NewRequest("POST", "/api/v1/dlq/replay", bytes.NewBufferString(body)))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp dlqReplayResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Replayed != 1 {
		t.Fatalf("replayed = %d, want 1", resp.Replayed)
	}
	if !resp.Overridden {
		t.Errorf("overridden = false, want true")
	}

	chain.mu.Lock()
	defer chain.mu.Unlock()
	if len(chain.states) != 1 || chain.states[0] != domain.StateSuccess {
		t.Errorf("states = %v, want [success]", chain.states)
	}
}

func TestDLQ_ReplayOverride_Timestamp(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()
	ctx := context.Background()

	env := dlqTestEnvelope(testEvtOrig)
	env.Report.State = domain.StateFailure
	if err := queue.Enqueue(ctx, env, errors.New("permanent")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	chain := &stateCapturingChain{}
	pipeline.Build(chain)

	ts := "2025-01-01T00:00:00Z"
	body := fmt.Sprintf(`{"override":{"timestamp":"%s"}}`, ts)
	rec := httptest.NewRecorder()
	dlqReplayHandler(queue, chain, nil, log)(rec, httptest.NewRequest("POST", "/api/v1/dlq/replay", bytes.NewBufferString(body)))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp dlqReplayResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Replayed != 1 {
		t.Fatalf("replayed = %d, want 1", resp.Replayed)
	}
	if !resp.Overridden {
		t.Errorf("overridden = false, want true")
	}
}

//nolint:dupl
func TestDLQReplayNoOverride(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()
	ctx := context.Background()

	env := dlqTestEnvelope("evt-1")
	env.Report.State = domain.StateFailure
	if err := queue.Enqueue(ctx, env, errors.New("permanent")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	chain := &stateCapturingChain{}
	pipeline.Build(chain)

	rec := httptest.NewRecorder()
	dlqReplayHandler(queue, chain, nil, log)(rec, httptest.NewRequest("POST", "/api/v1/dlq/replay", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp dlqReplayResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Replayed != 1 {
		t.Fatalf("replayed = %d, want 1", resp.Replayed)
	}
	if resp.Overridden {
		t.Errorf("overridden = true, want false")
	}

	chain.mu.Lock()
	defer chain.mu.Unlock()
	if len(chain.states) != 1 || chain.states[0] != domain.StateFailure {
		t.Errorf("states = %v, want [failure]", chain.states)
	}
}

func TestDLQReplayInvalidTimestamp(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()

	body := `{"override":{"timestamp":"not-a-timestamp"}}`
	rec := httptest.NewRecorder()
	dlqReplayHandler(queue, nil, nil, log)(rec, httptest.NewRequest("POST", "/api/v1/dlq/replay", bytes.NewBufferString(body)))

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

//nolint:dupl
func TestDLQ_ReplayOverride_EmptyOverrideObject(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()
	ctx := context.Background()

	env := dlqTestEnvelope("evt-1")
	env.Report.State = domain.StateFailure
	if err := queue.Enqueue(ctx, env, errors.New("permanent")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	chain := &stateCapturingChain{}
	pipeline.Build(chain)

	body := `{"override":{}}`
	rec := httptest.NewRecorder()
	dlqReplayHandler(queue, chain, nil, log)(rec, httptest.NewRequest("POST", "/api/v1/dlq/replay", bytes.NewBufferString(body)))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp dlqReplayResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Replayed != 1 {
		t.Fatalf("replayed = %d, want 1", resp.Replayed)
	}
	if !resp.Overridden {
		t.Errorf("overridden = false, want true")
	}

	chain.mu.Lock()
	defer chain.mu.Unlock()
	if len(chain.states) != 1 || chain.states[0] != domain.StateFailure {
		t.Errorf("states = %v, want [failure] (no payload override)", chain.states)
	}
}

func TestDLQReplayWithFilter(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	log := zap.NewNop()
	ctx := context.Background()

	env1 := dlqTestEnvelope("evt-1")
	env1.Report.Provider = "github"
	env1.Report.State = domain.StateFailure
	if err := queue.Enqueue(ctx, env1, errors.New("perm")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	env2 := dlqTestEnvelope("evt-2")
	env2.Report.Provider = providerGitLab
	env2.Report.State = domain.StateFailure
	if err := queue.Enqueue(ctx, env2, errors.New("perm")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	chain := &stateCapturingChain{}
	pipeline.Build(chain)

	body := `{"filter":{"provider":"github"},"override":{"payload":{"state":"success"}}}`
	rec := httptest.NewRecorder()
	dlqReplayHandler(queue, chain, nil, log)(rec, httptest.NewRequest("POST", "/api/v1/dlq/replay", bytes.NewBufferString(body)))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp dlqReplayResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Replayed != 1 {
		t.Fatalf("replayed = %d, want 1 (only github filtered)", resp.Replayed)
	}
	if !resp.Filtered {
		t.Errorf("filtered = false, want true")
	}
	if !resp.Overridden {
		t.Errorf("overridden = false, want true")
	}

	size, _ := queue.Size(ctx)
	if size != 1 {
		t.Fatalf("dlq size = %d, want 1 (gitlab event remains)", size)
	}

	chain.mu.Lock()
	defer chain.mu.Unlock()
	if len(chain.states) != 1 || chain.states[0] != domain.StateSuccess {
		t.Errorf("states = %v, want [success]", chain.states)
	}
}

func TestApplyOverride_DoesNotMutateOriginal(t *testing.T) {
	orig := dlq.DeadEvent{
		ID:       testEvtOrig,
		FailedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Envelope: &event.Envelope{
			CloudEventID: testEvtOrig,
			Report: domain.Event{
				State:   domain.StateFailure,
				RunName: "run-1",
			},
		},
	}

	o := replayOverride{
		Payload:   map[string]any{"state": "success"},
		Timestamp: "2025-06-01T12:00:00Z",
	}

	override, err := applyOverride(orig, o)
	if err != nil {
		t.Fatalf("applyOverride: %v", err)
	}

	if orig.ID != testEvtOrig {
		t.Errorf("original ID mutated: %s", orig.ID)
	}
	if orig.Envelope.Report.State != domain.StateFailure {
		t.Errorf("original State mutated: %s", orig.Envelope.Report.State)
	}
	if orig.Envelope.CloudEventID != testEvtOrig {
		t.Errorf("original CloudEventID mutated: %s", orig.Envelope.CloudEventID)
	}

	if override.ID == testEvtOrig {
		t.Errorf("override ID not changed")
	}
	if override.Envelope.Report.State != domain.StateSuccess {
		t.Errorf("override State = %s, want success", override.Envelope.Report.State)
	}
	if override.Envelope.CloudEventID != override.ID {
		t.Errorf("override CloudEventID = %s, want %s", override.Envelope.CloudEventID, override.ID)
	}
	if override.Envelope.Report.RunName != "run-1" {
		t.Errorf("override RunName = %s, want run-1 (preserved)", override.Envelope.Report.RunName)
	}
}

func appendLine(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // test helper, path is controlled
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		return err
	}
	_, err = f.Write([]byte("\n"))
	return err
}
