package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	s, err := OpenSQL("sqlite", SQLiteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	err = s.Update(func(st *State) error {
		st.Inbounds = append(st.Inbounds, Inbound{ID: "i1", Tag: "inbound-i1", Protocol: "vless", Listen: "0.0.0.0", Port: 443, Enable: true, Raw: json.RawMessage(`{"a":1}`), CreatedAt: now, UpdatedAt: now})
		st.Clients = append(st.Clients, Client{ID: "c1", InboundID: "i1", Email: "a@b", UUID: "u1", Enable: true, TotalBytes: 10, Up: 3, CreatedAt: now})
		st.Settings["k"] = "v"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = OpenSQL("sqlite", SQLiteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Inbounds) != 1 || len(snap.Clients) != 1 || snap.Settings["k"] != "v" {
		t.Fatalf("unexpected state: %+v", snap)
	}
	if snap.Inbounds[0].Port != 443 || !snap.Inbounds[0].Enable || string(snap.Inbounds[0].Raw) != `{"a":1}` {
		t.Fatalf("inbound mismatch: %+v", snap.Inbounds[0])
	}
	if !snap.Clients[0].CreatedAt.Equal(now) || snap.Clients[0].Up != 3 {
		t.Fatalf("client mismatch: %+v", snap.Clients[0])
	}

	err = s.Update(func(st *State) error {
		st.Clients[0].Up += 7
		st.Inbounds = nil
		delete(st.Settings, "k")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = OpenSQL("sqlite", SQLiteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snap = s.Snapshot()
	if len(snap.Inbounds) != 0 || len(snap.Settings) != 0 || snap.Clients[0].Up != 10 {
		t.Fatalf("update not persisted: %+v", snap)
	}
}
