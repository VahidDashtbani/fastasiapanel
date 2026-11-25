package xray

import (
	"context"
	"encoding/json"
	"errors"
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
	cfg       config.Config
	st        *store.Store
	mu        sync.Mutex
	cmd       *exec.Cmd
	done      chan struct{}
	running   atomic.Bool
	timer     *time.Timer
	liveMu    sync.RWMutex
	live      map[string]liveStat
	want      atomic.Bool
	lastErr   atomic.Value
	fails     int
	retryAt   time.Time
	startedAt time.Time
}

type liveStat struct {
	at       int64
	up, down int64
}

func (m *Manager) setLive(users map[string]delta, now int64) {
	m.liveMu.Lock()
	defer m.liveMu.Unlock()
	if m.live == nil {
		m.live = map[string]liveStat{}
	}
	for email, d := range users {
		if d.up > 0 || d.down > 0 {
			m.live[email] = liveStat{at: now, up: d.up, down: d.down}
		}
	}
	for email, s := range m.live {
		if now-s.at > 60000 {
			delete(m.live, email)
		}
	}
}

func (m *Manager) Live(email string) (up, down int64, online bool) {
	m.liveMu.RLock()
	s, ok := m.live[email]
	m.liveMu.RUnlock()
	if !ok {
		return 0, 0, false
	}
	age := time.Now().UnixMilli() - s.at
	if age > 10000 {
		return 0, 0, false
	}
	if age > 2500 {
		return 0, 0, true
	}
	return s.up, s.down, true
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

func (m *Manager) setErr(err error) {
	if err == nil {
		m.lastErr.Store("")
	} else {
		m.lastErr.Store(err.Error())
	}
}

func (m *Manager) LastError() string {
	s, _ := m.lastErr.Load().(string)
	return s
}

func (m *Manager) prepareConfig() (string, error) {
	data, err := BuildConfig(m.st.Snapshot())
	if err != nil {
		return "", err
	}
	tmp := strings.TrimSuffix(m.cfg.XrayConfig, ".json") + ".new.json"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.cfg.XrayBinary, "run", "-test", "-config", tmp).CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("xray rejected the config: %s", msg)
	}
	return tmp, nil
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
	m.mu.Lock()
	defer m.mu.Unlock()
	next, err := m.prepareConfig()
	if err != nil {
		m.setErr(err)
		return err
	}
	live := m.cfg.XrayConfig
	prev, readErr := os.ReadFile(live)
	if readErr == nil {
		_ = os.WriteFile(live+".last-good", prev, 0600)
	}
	if err = os.Rename(next, live); err != nil {
		m.setErr(err)
		return err
	}
	m.stopLocked()
	if err = m.startLocked(); err != nil {
		m.setErr(err)
		return err
	}
	m.want.Store(true)
	m.startedAt = time.Now()
	select {
	case <-m.done:
		err = errors.New("xray exited right after start")
		if readErr == nil {
			_ = os.WriteFile(live, prev, 0600)
			if m.startLocked() == nil {
				m.startedAt = time.Now()
				err = errors.New("xray exited right after start; previous config restored")
			}
		}
		m.setErr(err)
		return err
	case <-time.After(2 * time.Second):
	}
	m.setErr(nil)
	return nil
}

func (m *Manager) Restart() error {
	return m.Start()
}

func (m *Manager) Stop() {
	m.want.Store(false)
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

func (m *Manager) watchdog() {
	if !m.want.Load() {
		return
	}
	m.mu.Lock()
	if m.running.Load() {
		if m.fails > 0 && time.Since(m.startedAt) > 30*time.Second {
			m.fails = 0
		}
		m.mu.Unlock()
		return
	}
	if time.Now().Before(m.retryAt) {
		m.mu.Unlock()
		return
	}
	m.fails++
	n := m.fails
	if n > 10 {
		n = 10
	}
	m.retryAt = time.Now().Add(time.Duration(n) * 3 * time.Second)
	m.mu.Unlock()
	log.Printf("xray is down, restarting (attempt %d)", n)
	if err := m.Restart(); err != nil {
		log.Printf("watchdog restart failed: %v", err)
	}
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
	m.setLive(userDeltas, now)
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
			m.watchdog()
			m.tick()
		}
	}
}
