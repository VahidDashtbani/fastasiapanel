package traffic

import (
	"path/filepath"
	"testing"
	"time"

	"fastasiapanel/internal/store"
	"fastasiapanel/internal/xray"
)

type fakeStats struct {
	values     []xray.TrafficStat
	generation uint64
}

func (f *fakeStats) QueryStats() ([]xray.TrafficStat, error) { return f.values, nil }
func (f *fakeStats) Generation() uint64                      { return f.generation }

type fakeApply struct{ calls int }

func (f *fakeApply) Apply(store.State) error { f.calls++; return nil }

func TestTrafficAccountingAndQuota(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Update(func(s *store.State) error {
		s.Clients = []store.Client{{ID: "c1", Email: "demo", Enable: true, TotalBytes: 100}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source := &fakeStats{values: []xray.TrafficStat{{Name: "user>>>demo>>>traffic>>>uplink", Value: 40}, {Name: "user>>>demo>>>traffic>>>downlink", Value: 70}}}
	applier := &fakeApply{}
	service := New(db, source, applier, time.Second)
	if err = service.Tick(); err != nil {
		t.Fatal(err)
	}
	got := db.Snapshot().Clients[0]
	if got.Up != 40 || got.Down != 70 || got.Enable {
		t.Fatalf("unexpected accounting state: %+v", got)
	}
	if applier.calls != 1 {
		t.Fatalf("expected one config apply, got %d", applier.calls)
	}
}

func TestTrafficAccountingUsesCounterDeltas(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Update(func(s *store.State) error {
		s.Clients = []store.Client{{ID: "c1", Email: "demo", Enable: true}}
		return nil
	})
	source := &fakeStats{values: []xray.TrafficStat{{Name: "user>>>demo>>>traffic>>>uplink", Value: 100}}}
	service := New(db, source, &fakeApply{}, time.Second)
	if err = service.Tick(); err != nil {
		t.Fatal(err)
	}
	source.values[0].Value = 150
	if err = service.Tick(); err != nil {
		t.Fatal(err)
	}
	if got := db.Snapshot().Clients[0].Up; got != 150 {
		t.Fatalf("expected 150 total bytes, got %d", got)
	}
}

func TestExpiredClientsAreDisabled(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Update(func(s *store.State) error {
		s.Clients = []store.Client{{ID: "c1", Email: "demo", Enable: true, ExpiryAt: time.Now().Add(-time.Minute).UnixMilli()}}
		return nil
	})
	service := New(db, &fakeStats{}, &fakeApply{}, time.Second)
	if err = service.Tick(); err != nil {
		t.Fatal(err)
	}
	if db.Snapshot().Clients[0].Enable {
		t.Fatal("expired client remained enabled")
	}
}

func TestTrafficAccountingResetsBaselineAfterXrayRestart(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Update(func(s *store.State) error {
		s.Clients = []store.Client{{ID: "c1", Email: "demo", Enable: true}}
		return nil
	})
	source := &fakeStats{values: []xray.TrafficStat{{Name: "user>>>demo>>>traffic>>>uplink", Value: 900}}, generation: 1}
	service := New(db, source, &fakeApply{}, time.Second)
	if err = service.Tick(); err != nil {
		t.Fatal(err)
	}
	source.generation = 2
	source.values[0].Value = 25
	if err = service.Tick(); err != nil {
		t.Fatal(err)
	}
	if got := db.Snapshot().Clients[0].Up; got != 925 {
		t.Fatalf("expected restart-safe total 925, got %d", got)
	}
}
