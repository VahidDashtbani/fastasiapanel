package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestJSONRoundTripAndRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	err = s.Update(func(st *State) error {
		st.Settings["a"] = "b"
		st.Inbounds = append(st.Inbounds, Inbound{ID: "i1", Tag: "in-i1", Protocol: "vless", Port: 443, Enable: true, CreatedAt: now, UpdatedAt: now})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Inbounds) != 1 || snap.Settings["a"] != "b" || snap.Inbounds[0].Port != 443 {
		t.Fatalf("state was not persisted: %+v", snap)
	}

	err = s.Update(func(st *State) error {
		st.Settings["a"] = "changed"
		st.Inbounds[0].Port = 1
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected the update error to be returned")
	}
	after := s.Snapshot()
	if s.Setting("a") != "b" || after.Inbounds[0].Port != 443 {
		t.Fatalf("a failed update must not change state: %+v", after)
	}
}

func TestSnapshotIsIsolated(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Update(func(st *State) error {
		st.Clients = append(st.Clients, Client{ID: "c", Email: "x"})
		return nil
	})
	snap := s.Snapshot()
	snap.Clients[0].Email = "mutated"
	if s.Snapshot().Clients[0].Email != "x" {
		t.Fatal("snapshot must be a deep copy")
	}
}
