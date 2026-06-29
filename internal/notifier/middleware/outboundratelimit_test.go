package middleware

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

func TestRateLimitMiddleware_NilLimiter(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}
	handler := WrapWithRateLimit(inner, nil)

	if handler != inner {
		t.Error("expected original handler returned when limiter is nil")
	}
}

func TestRateLimitMiddleware_DelegatesNameAndType(t *testing.T) {
	inner := &mockActionHandler{name: "github", typ: notifier.ActionPRComment}
	limiter := rate.NewLimiter(rate.Limit(100), 100)
	handler := WrapWithRateLimit(inner, limiter)

	if handler.Name() != "github" {
		t.Errorf("expected name=github, got %s", handler.Name())
	}
	if handler.Type() != notifier.ActionPRComment {
		t.Errorf("expected type=pr_comment, got %s", handler.Type())
	}
}

func TestRateLimitMiddleware_PassesThrough(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}
	limiter := rate.NewLimiter(rate.Limit(100), 100)
	handler := WrapWithRateLimit(inner, limiter)

	event := domain.Event{RunName: testRunName}
	err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !inner.called {
		t.Error("expected inner handler to be called")
	}
}

func TestRateLimitMiddleware_ThrottlesRequests(t *testing.T) {
	var calls atomic.Int64
	inner := &countingHandler{name: testHandler, typ: notifier.ActionCommitStatus, calls: &calls}
	limiter := rate.NewLimiter(rate.Limit(10), 1) // 10 rps, burst 1
	handler := WrapWithRateLimit(inner, limiter)

	ctx := context.Background()
	event := domain.Event{RunName: testRunName}

	// First request should pass immediately (burst=1)
	start := time.Now()
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Error("first request should not be delayed")
	}

	// Second request should be delayed ~100ms (1/10 rps)
	start = time.Now()
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected delay >= 50ms, got %v", elapsed)
	}
}

func TestRateLimitMiddleware_PropagatesError(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus, err: context.Canceled}
	limiter := rate.NewLimiter(rate.Limit(100), 100)
	handler := WrapWithRateLimit(inner, limiter)

	err := handler.Handle(context.Background(), domain.Event{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRateLimitMiddleware_ContextCancellation(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}
	limiter := rate.NewLimiter(rate.Limit(0.001), 1) // extremely slow
	handler := WrapWithRateLimit(inner, limiter)

	// Exhaust the burst
	if err := handler.Handle(context.Background(), domain.Event{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := handler.Handle(ctx, domain.Event{})
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestRateLimitMiddleware_CloseDelegates(t *testing.T) {
	inner := &mockActionHandler{name: testHandler, typ: notifier.ActionCommitStatus}
	limiter := rate.NewLimiter(rate.Limit(100), 100)
	handler := WrapWithRateLimit(inner, limiter)

	if err := handler.Close(); err != nil {
		t.Errorf("expected nil error from Close, got %v", err)
	}
}

type countingHandler struct {
	name  string
	typ   notifier.ActionType
	calls *atomic.Int64
}

func (h *countingHandler) Name() string              { return h.name }
func (h *countingHandler) Type() notifier.ActionType { return h.typ }
func (h *countingHandler) Handle(_ context.Context, _ domain.Event) error {
	h.calls.Add(1)
	return nil
}
func (h *countingHandler) Close() error { return nil }
