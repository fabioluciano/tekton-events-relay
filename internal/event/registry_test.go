package event

import (
	"errors"
	"testing"
)

var errFakeDecode = errors.New("fake decode error")

type fakeDecoder struct {
	name   string
	prefix string
}

func (f *fakeDecoder) Name() string                         { return f.name }
func (f *fakeDecoder) CanHandle(t string) bool              { return len(t) > 0 && t[:1] == f.prefix }
func (f *fakeDecoder) Decode(_ RawEvent) (*Envelope, error) { return nil, errFakeDecode }

func TestRegistry_FindMatchesByOrder(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeDecoder{name: "tekton", prefix: "d"})
	r.Register(&fakeDecoder{name: "foreign-engine", prefix: "i"})

	d, err := r.Find("dev.tekton.event.pipelinerun.started.v1")
	if err != nil {
		t.Fatalf("Find err: %v", err)
	}
	if d.Name() != "tekton" {
		t.Errorf("got %q, want tekton", d.Name())
	}

	d, err = r.Find("io.example.foreign.v1.completed")
	if err != nil {
		t.Fatalf("Find err: %v", err)
	}
	if d.Name() != "foreign-engine" {
		t.Errorf("got %q, want foreign-engine", d.Name())
	}
}

func TestRegistry_FindUnknown(t *testing.T) {
	r := NewRegistry()
	_, err := r.Find("foo.bar")
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeDecoder{name: "tekton", prefix: "d"})
	r.Register(&fakeDecoder{name: "argo", prefix: "a"})
	r.Register(&fakeDecoder{name: "flux", prefix: "f"})

	names := r.Names()
	if len(names) != 3 {
		t.Errorf("Names() returned %d decoders, want 3", len(names))
	}

	// Check all expected names are present
	expected := map[string]bool{"tekton": true, "argo": true, "flux": true}
	for _, name := range names {
		if !expected[name] {
			t.Errorf("unexpected decoder name: %q", name)
		}
		delete(expected, name)
	}
	if len(expected) > 0 {
		t.Errorf("missing decoder names: %v", expected)
	}
}
