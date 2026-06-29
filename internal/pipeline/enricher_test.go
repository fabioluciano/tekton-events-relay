package pipeline

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
)

func enricherSample(resource domain.Resource) *event.Envelope {
	return &event.Envelope{
		CloudEventID: "enricher-test",
		Report: domain.Event{
			Provider:  testProviderGitHub,
			Resource:  resource,
			Namespace: "ns",
			RunName:   "run-1",
			CommitSHA: "abc",
			State:     domain.StateSuccess,
		},
	}
}

func TestEnricher_CustomRunDashboardLink(t *testing.T) {
	e := NewEnricher(testTektonURL)
	env := enricherSample(domain.ResourceCustomRun)
	if err := e.Handle(context.Background(), env); err != nil {
		t.Fatalf(testUnexpectedErr, err)
	}
	want := testTektonURL + "/#/namespaces/ns/customruns/run-1"
	if env.Report.TargetURL != want {
		t.Errorf("TargetURL = %q, want %q", env.Report.TargetURL, want)
	}
}

func TestEnricher_TaskRunDashboardLink(t *testing.T) {
	e := NewEnricher(testTektonURL)
	env := enricherSample(domain.ResourceTaskRun)
	if err := e.Handle(context.Background(), env); err != nil {
		t.Fatalf(testUnexpectedErr, err)
	}
	want := testTektonURL + "/#/namespaces/ns/taskruns/run-1"
	if env.Report.TargetURL != want {
		t.Errorf("TargetURL = %q, want %q", env.Report.TargetURL, want)
	}
}

func TestEnricher_EventListenerHasNoDashboardLink(t *testing.T) {
	e := NewEnricher(testTektonURL)
	env := enricherSample(domain.ResourceEventListener)
	if err := e.Handle(context.Background(), env); err != nil {
		t.Fatalf(testUnexpectedErr, err)
	}
	if env.Report.TargetURL != "" {
		t.Errorf("TargetURL = %q, want empty for eventlistener", env.Report.TargetURL)
	}
}

func TestDeduper_UpdatesCacheSizeGauge(t *testing.T) {
	collectors := metrics.NewCollectors(prometheus.NewRegistry())
	d := newMemDeduper(100, collectors)
	Build(d, &terminal{})

	for _, id := range []string{testEventID1, testEventID2} {
		if err := d.Handle(context.Background(), sample(id)); err != nil {
			t.Fatalf(testUnexpectedErr, err)
		}
	}

	if got := testutil.ToFloat64(collectors.DedupeCacheSize); got != 2 {
		t.Errorf("dedupe_cache_size = %v, want 2", got)
	}

	// A duplicate must not grow the gauge.
	if err := d.Handle(context.Background(), sample(testEventID1)); err != nil {
		t.Fatalf(testUnexpectedErr, err)
	}
	if got := testutil.ToFloat64(collectors.DedupeCacheSize); got != 2 {
		t.Errorf("dedupe_cache_size after duplicate = %v, want 2", got)
	}
}
