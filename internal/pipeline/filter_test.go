package pipeline

import (
	"context"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func TestEventFilter_NamespaceAllowed_EmptyLists(t *testing.T) {
	f := NewEventFilter(true, true, true, true, false, nil, nil)
	term := &terminal{}
	Build(f, term)

	env := sample("filter-ns")
	env.Report.Namespace = testNamespaceProd
	if err := f.Handle(context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if term.count != 1 {
		t.Errorf("expected event to pass with empty namespace lists, got count=%d", term.count)
	}
}

func TestEventFilter_NamespaceAllowed_MatchesAllow(t *testing.T) {
	f := NewEventFilter(true, true, true, true, false, []string{testNamespaceProd, "staging"}, nil)
	term := &terminal{}
	Build(f, term)

	env := sample("filter-ns-allow")
	env.Report.Namespace = testNamespaceProd
	if err := f.Handle(context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if term.count != 1 {
		t.Errorf("expected 'production' namespace to be allowed, got count=%d", term.count)
	}
}

func TestEventFilter_NamespaceAllowed_RejectsNotInAllow(t *testing.T) {
	f := NewEventFilter(true, true, true, true, false, []string{testNamespaceProd}, nil)
	term := &terminal{}
	Build(f, term)

	env := sample("filter-ns-reject")
	env.Report.Namespace = "staging"
	if err := f.Handle(context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if term.count != 0 {
		t.Errorf("expected 'staging' namespace to be rejected when allow=[production], got count=%d", term.count)
	}
}

func TestEventFilter_NamespaceAllowed_DenyWins(t *testing.T) {
	f := NewEventFilter(true, true, true, true, false,
		[]string{"*"},         // allow all
		[]string{"denied-ns"}, // but deny this specific ns
	)
	term := &terminal{}
	Build(f, term)

	env := sample("filter-ns-deny")
	env.Report.Namespace = "denied-ns"
	if err := f.Handle(context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if term.count != 0 {
		t.Errorf("expected 'denied-ns' namespace to be denied, got count=%d", term.count)
	}
}

func TestEventFilter_NamespaceAllowed_DenyOverAllow(t *testing.T) {
	f := NewEventFilter(true, true, true, true, false,
		[]string{testNamespaceProd}, // allow production
		[]string{testNamespaceProd}, // but also deny it — deny wins
	)
	term := &terminal{}
	Build(f, term)

	env := sample("filter-ns-deny-over-allow")
	env.Report.Namespace = testNamespaceProd
	if err := f.Handle(context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if term.count != 0 {
		t.Errorf("expected deny to win over allow, got count=%d", term.count)
	}
}

func TestEventFilter_NamespaceAllowed_WildcardPattern(t *testing.T) {
	f := NewEventFilter(true, true, true, true, false, []string{"*.production"}, nil)
	term := &terminal{}
	Build(f, term)

	tests := []struct {
		name      string
		namespace string
		pass      bool
	}{
		{"matching wildcard", "us-east.production", true},
		{"non-matching", "staging", false},
		{"non-matching suffix", "production.staging", false},
		{"exact match dot", ".production", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := sample("filter-ns-wildcard-" + tt.name)
			env.Report.Namespace = tt.namespace
			// Use a fresh terminal per subtest
			subTerm := &terminal{}
			subFilter := NewEventFilter(true, true, true, true, false, []string{"*.production"}, nil)
			Build(subFilter, subTerm)

			if err := subFilter.Handle(context.Background(), env); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.pass && subTerm.count != 1 {
				t.Errorf("expected namespace %q to pass, got count=%d", tt.namespace, subTerm.count)
			}
			if !tt.pass && subTerm.count != 0 {
				t.Errorf("expected namespace %q to be filtered, got count=%d", tt.namespace, subTerm.count)
			}
		})
	}
}

func TestEventFilter_NamespaceAllowed_DenyWildcard(t *testing.T) {
	f := NewEventFilter(true, true, true, true, false, nil, []string{"kube-*"})
	term := &terminal{}
	Build(f, term)

	tests := []struct {
		name      string
		namespace string
		pass      bool
	}{
		{"deny kube-system", "kube-system", false},
		{"deny kube-public", "kube-public", false},
		{"allow default", "default", true},
		{"allow production", testNamespaceProd, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := sample("filter-ns-deny-wildcard-" + tt.name)
			env.Report.Namespace = tt.namespace
			subTerm := &terminal{}
			subFilter := NewEventFilter(true, true, true, true, false, nil, []string{"kube-*"})
			Build(subFilter, subTerm)

			if err := subFilter.Handle(context.Background(), env); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.pass && subTerm.count != 1 {
				t.Errorf("expected namespace %q to pass, got count=%d", tt.namespace, subTerm.count)
			}
			if !tt.pass && subTerm.count != 0 {
				t.Errorf("expected namespace %q to be denied, got count=%d", tt.namespace, subTerm.count)
			}
		})
	}
}

func TestEventFilter_NamespaceAllowed_DenyAll(t *testing.T) {
	f := NewEventFilter(true, true, true, true, false, nil, []string{"*"})
	term := &terminal{}
	Build(f, term)

	env := sample("filter-ns-deny-all")
	env.Report.Namespace = "anything"
	if err := f.Handle(context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if term.count != 0 {
		t.Errorf("expected all namespaces to be denied, got count=%d", term.count)
	}
}

func TestEventFilter_NamespaceAllowed_ResourceTypeStillWorks(t *testing.T) {
	f := NewEventFilter(false, true, false, false, false, nil, nil)
	term := &terminal{}
	Build(f, term)

	env := sample("filter-ns-resource")
	env.Report.Namespace = "any-ns"
	if err := f.Handle(context.Background(), env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if term.count != 1 {
		t.Errorf("expected PipelineRun to pass with no namespace constraints, got count=%d", term.count)
	}
}

// Ensure the Namespace field exists in domain.Event (compile-time check).
var _ = domain.Event{Namespace: "compile-check"}
