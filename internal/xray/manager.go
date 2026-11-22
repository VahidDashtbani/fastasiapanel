package xray

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fastasiapanel/internal/config"
	"fastasiapanel/internal/store"
)

type Manager struct {
	cfg     config.Config
	st      *store.Store
	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan struct{}
	running atomic.Bool
	timer   *time.Timer
}

func NewManager(cfg config.Config, st *store.Store) *Manager {
	return &Manager{cfg: cfg, st: st}
}

func (m *Manager) Running() bool {
	return m.running.Load()
}

func (m *Manager) Version() string {
	out, err := exec.Command(m.cfg.XrayBinary, "version").Output()
	if err != nil {
		return "unknown"
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return "unknown"
}

func (m *Manager) writeConfig() error {
	data, err := BuildConfig(m.st.Snapshot())
	if err != nil {
		return err
	}
	tmp := strings.TrimSuffix(m.cfg.XrayConfig, ".json") + ".new.json"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	out, err := exec.Command(m.cfg.XrayBinary, "run", "-test", "-config", tmp).CombinedOutput()
	if err != nil {
		os.Remove(tmp)
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("xray rejected the config: %s", msg)
	}
	return os.Rename(tmp, m.cfg.XrayConfig)
}

func (m *Manager) stopLocked() {
	if m.cmd == nil {
		return
	}
	if m.running.Load() && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		select {
		case <-m.done:
		case <-time.After(5 * time.Second):
		}
	}
	m.running.Store(false)
	m.cmd = nil
}

func (m *Manager) startLocked() error {
	cmd := exec.Command(m.cfg.XrayBinary, "run", "-config", m.cfg.XrayConfig)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	m.cmd, m.done = cmd, done
	m.running.Store(true)
	go func() {
		_ = cmd.Wait()
		close(done)
		m.mu.Lock()
		if m.cmd == cmd {
			m.running.Store(false)
		}
		m.mu.Unlock()
		log.Println("xray process exited")
	}()
	return nil
}

func (m *Manager) Start() error {
	if err := m.writeConfig(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
	return m.startLocked()
}

func (m *Manager) Restart() error {
	return m.Start()
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		m.timer.Stop()
	}
	m.stopLocked()
}

func (m *Manager) Schedule() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(1500*time.Millisecond, func() {
		if err := m.Restart(); err != nil {
			log.Printf("xray restart failed: %v", err)
		}
	})
}

type delta struct{ up, down int64 }

func (m *Manager) queryStats() (map[string]delta, map[string]delta) {
	out, err := exec.Command(m.cfg.XrayBinary, "api", "statsquery", "-s", apiAddr, "-reset").Output()
	if err != nil {
		return nil, nil
	}
	var res struct {
		Stat []struct {
			Name  string      `json:"name"`
			Value json.Number `json:"value"`
		} `json:"stat"`
	}
	if json.Unmarshal(out, &res) != nil {
		return nil, nil
	}
	userDeltas := map[string]delta{}
	inboundDeltas := map[string]delta{}
	for _, s := range res.Stat {
		p := strings.Split(s.Name, ">>>")
		if len(p) != 4 {
			continue
		}
		v, _ := s.Value.Int64()
		if v == 0 {
			continue
		}
		switch p[0] {
		case "user":
			d := userDeltas[p[1]]
			if p[3] == "uplink" {
				d.up += v
			} else {
				d.down += v
			}
			userDeltas[p[1]] = d
		case "inbound":
			d := inboundDeltas[p[1]]
			if p[3] == "uplink" {
				d.up += v
			} else {
				d.down += v
			}
			inboundDeltas[p[1]] = d
		}
	}
	return userDeltas, inboundDeltas
}

func shouldResetClient(c store.Client, now int64) bool {
	if c.Reset <= 0 {
		return false
	}
	base := c.LastResetAt
	if base == 0 {
		base = c.CreatedAt.UnixMilli()
	}
	if base <= 0 {
		return false
	}
	intervalMs := int64(c.Reset) * 24 * int64(time.Hour/time.Millisecond)
	return now-base >= intervalMs
}

func (m *Manager) tick() {
	if !m.Running() {
		return
	}
	userDeltas, inboundDeltas := m.queryStats()
	now := time.Now().UnixMilli()
	need := len(userDeltas) > 0 || len(inboundDeltas) > 0

	if !need {
		snap := m.st.Snapshot()
		for _, c := range snap.Clients {
			if (c.Enable && c.ShouldDisable(now)) || shouldResetClient(c, now) {
				need = true
				break
			}
		}
		if !need {
			for _, in := range snap.Inbounds {
				if in.Enable && ((in.TotalBytes > 0 && in.Up+in.Down >= in.TotalBytes) || (in.ExpiryAt > 0 && now >= in.ExpiryAt)) {
					need = true
					break
				}
			}
		}
	}
	if !need {
		return
	}

	disabled := 0
	err := m.st.Update(func(s *store.State) error {
		for i := range s.Inbounds {
			in := &s.Inbounds[i]
			if d, ok := inboundDeltas[in.Tag]; ok {
				in.Up += d.up
				in.Down += d.down
			}
			if in.Enable {
				if (in.TotalBytes > 0 && in.Up+in.Down >= in.TotalBytes) || (in.ExpiryAt > 0 && now >= in.ExpiryAt) {
					in.Enable = false
					disabled++
				}
			}
		}
		for i := range s.Clients {
			c := &s.Clients[i]
			if shouldResetClient(*c, now) {
				c.Up = 0
				c.Down = 0
				c.LastResetAt = now
				if !c.IsExpired(now) {
					c.Enable = true
				}
			}
			if d, ok := userDeltas[c.Email]; ok {
				c.Up += d.up
				c.Down += d.down
				if c.FirstSeenAt == 0 && (d.up > 0 || d.down > 0) {
					c.FirstSeenAt = now
				}
			}
			if c.Enable && c.ShouldDisable(now) {
				c.Enable = false
				disabled++
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("stats update failed: %v", err)
		return
	}
	if disabled > 0 {
		log.Printf("disabled %d expired or over-limit entries", disabled)
		m.Schedule()
	}
}

func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.tick()
		}
	}
}