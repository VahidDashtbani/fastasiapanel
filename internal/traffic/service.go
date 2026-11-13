package traffic

import (
	"log"
	"strings"
	"sync"
	"time"

	"fastasiapanel/internal/store"
	"fastasiapanel/internal/xray"
)

type StatsSource interface {
	QueryStats() ([]xray.TrafficStat, error)
	Generation() uint64
}

type ConfigApplier interface {
	Apply(store.State) error
}

type Service struct {
	store      *store.Store
	source     StatsSource
	apply      ConfigApplier
	mu         sync.Mutex
	last       map[string]int64
	generation uint64
	needsApply bool
	period     time.Duration
}

func New(db *store.Store, source StatsSource, apply ConfigApplier, period time.Duration) *Service {
	if period <= 0 {
		period = 10 * time.Second
	}
	return &Service{store: db, source: source, apply: apply, last: map[string]int64{}, period: period}
}

func (s *Service) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(s.period)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := s.Tick(); err != nil {
				log.Printf("traffic accounting: %v", err)
			}
		}
	}
}

func (s *Service) Tick() error {
	stats, queryErr := s.source.QueryStats()
	s.mu.Lock()
	defer s.mu.Unlock()
	generation := s.source.Generation()
	if generation != s.generation {
		s.last = map[string]int64{}
		s.generation = generation
	}
	nextLast := make(map[string]int64, len(s.last))
	for key, value := range s.last {
		nextLast[key] = value
	}
	deltas := map[string]map[string]int64{}
	for _, stat := range stats {
		parts := strings.Split(stat.Name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		key := stat.Name
		previous := nextLast[key]
		delta := stat.Value - previous
		if delta < 0 || stat.Value < 0 {
			delta = stat.Value
		}
		nextLast[key] = stat.Value
		if delta == 0 {
			continue
		}
		if deltas[parts[1]] == nil {
			deltas[parts[1]] = map[string]int64{}
		}
		if parts[3] == "uplink" {
			deltas[parts[1]]["up"] += delta
		} else if parts[3] == "downlink" {
			deltas[parts[1]]["down"] += delta
		}
	}
	changed := false
	now := time.Now().UnixMilli()
	err := s.store.Update(func(st *store.State) error {
		for i := range st.Clients {
			c := &st.Clients[i]
			if d := deltas[c.Email]; d != nil {
				c.Up += d["up"]
				c.Down += d["down"]
			}
			if c.Enable && ((c.ExpiryAt > 0 && c.ExpiryAt <= now) || (c.TotalBytes > 0 && c.Up+c.Down >= c.TotalBytes)) {
				c.Enable = false
				changed = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.last = nextLast
	if changed {
		s.needsApply = true
	}
	if s.needsApply {
		if err := s.apply.Apply(s.store.Snapshot()); err != nil {
			return err
		}
		s.needsApply = false
	}
	return queryErr
}
