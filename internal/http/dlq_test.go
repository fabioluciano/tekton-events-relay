package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/dlq"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
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
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0)
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
		t.Errorf("dlq size after replay = %d, want 0", size)
	}
}

func TestDLQ_ReplayKeepsFailingEvents(t *testing.T) {
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0)
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
	queue, _ := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0)
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
	queue, err := dlq.NewFileQueue(filepath.Join(t.TempDir(), "dlq.jsonl"), 0)
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
