package msgstore

import (
	"testing"
	"time"
)

func TestMemoryStore_SaveLoad_RoundTrip(t *testing.T) {
	s := NewMemoryStore(time.Minute, 10)

	s.Save("run-1", "111.111")
	got, ok := s.Load("run-1")
	if !ok {
		t.Fatal("expected stored reference, got miss")
	}
	if got != "111.111" {
		t.Errorf("Load = %q, want 111.111", got)
	}
}

func TestMemoryStore_Load_Miss(t *testing.T) {
	s := NewMemoryStore(time.Minute, 10)

	if _, ok := s.Load("absent"); ok {
		t.Error("expected miss for absent key")
	}
}

func TestMemoryStore_Save_Replaces(t *testing.T) {
	s := NewMemoryStore(time.Minute, 10)

	s.Save("run-1", "111.111")
	s.Save("run-1", "222.222")

	got, ok := s.Load("run-1")
	if !ok {
		t.Fatal("expected stored reference, got miss")
	}
	if got != "222.222" {
		t.Errorf("Load = %q, want 222.222 (replaced)", got)
	}
}

func TestNewMemoryStore_Defaults(t *testing.T) {
	s := NewMemoryStore(0, 0)

	s.Save("run-1", "111.111")
	if _, ok := s.Load("run-1"); !ok {
		t.Error("expected store with defaults to retain entry")
	}
}
