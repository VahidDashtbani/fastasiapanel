package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdatePersistsAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "panel.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Update(func(st *State) error { st.Settings["theme"] = "dark"; return nil }); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Snapshot().Settings["theme"] != "dark" {
		t.Fatalf("unexpected state: %s", b)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestNewUUIDFormat(t *testing.T) {
	id, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 || id[14] != '4' {
		t.Fatalf("invalid UUID: %s", id)
	}
}

func TestFailedUpdateDoesNotMutateLiveState(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("abort")
	if err = s.Update(func(st *State) error { st.Settings["partial"] = "must-not-commit"; return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := s.Snapshot().Settings["partial"]; ok {
		t.Fatal("failed transaction mutated live state")
	}
}
