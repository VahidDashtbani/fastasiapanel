package xray

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"fastasiapanel/internal/store"
)

type Manager struct {
	mu         sync.Mutex
	binary     string
	config     string
	cmd        *exec.Cmd
	done       chan error
	started    time.Time
	generation uint64
	lastError  string
}
type TrafficStat struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

func (s *TrafficStat) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Name = raw.Name
	if len(raw.Value) == 0 {
		s.Value = 0
		return nil
	}
	if err := json.Unmarshal(raw.Value, &s.Value); err == nil {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw.Value, &text); err != nil {
		return err
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return err
	}
	s.Value = value
	return nil
}

type Status struct {
	Running   bool   `json:"running"`
	PID       int    `json:"pid"`
	Uptime    int64  `json:"uptime_seconds"`
	LastError string `json:"last_error"`
}

func New(binary, config string) *Manager { return &Manager{binary: binary, config: config} }
func (m *Manager) Build(st store.State) ([]byte, error) {
	in := make([]json.RawMessage, 0, len(st.Inbounds))
	for _, v := range st.Inbounds {
		if !v.Enable {
			continue
		}
		if v.Port == 10085 {
			return nil, errors.New("inbound port 10085 is reserved for the Xray API")
		}
		if !json.Valid(v.Raw) {
			return nil, fmt.Errorf("inbound %s has invalid raw JSON", v.Tag)
		}
		var root map[string]json.RawMessage
		if err := json.Unmarshal(v.Raw, &root); err != nil {
			return nil, err
		}
		var settings map[string]json.RawMessage
		if err := json.Unmarshal(root["settings"], &settings); err != nil {
			return nil, fmt.Errorf("inbound %s settings must be an object", v.Tag)
		}
		if settings == nil {
			settings = map[string]json.RawMessage{}
		}
		clientKey := "clients"
		if v.Protocol == "socks" || v.Protocol == "http" {
			clientKey = "accounts"
		}
		var clients []json.RawMessage
		if existing := settings[clientKey]; len(existing) > 0 {
			if err := json.Unmarshal(existing, &clients); err != nil {
				return nil, fmt.Errorf("inbound %s has invalid %s list", v.Tag, clientKey)
			}
		}
		managedEmails := map[string]bool{}
		for _, rawClient := range clients {
			var existing map[string]any
			if json.Unmarshal(rawClient, &existing) == nil {
				if email, ok := existing["email"].(string); ok && email != "" {
					managedEmails[email] = true
				}
			}
		}
		for _, client := range st.Clients {
			if client.InboundID != v.ID || !client.Enable {
				continue
			}
			if managedEmails[client.Email] {
				return nil, fmt.Errorf("inbound %s already contains a raw client with email %s", v.Tag, client.Email)
			}
			if len(client.Config) > 0 {
				if !json.Valid(client.Config) {
					return nil, fmt.Errorf("client %s has invalid config", client.Email)
				}
				clients = append(clients, client.Config)
			}
		}
		if len(clients) > 0 {
			settings[clientKey], _ = json.Marshal(clients)
			root["settings"], _ = json.Marshal(settings)
		}
		merged, err := json.Marshal(root)
		if err != nil {
			return nil, err
		}
		in = append(in, merged)
	}
	apiInbound := map[string]any{"listen": "127.0.0.1", "port": 10085, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}, "tag": "api"}
	in = append(in, mustJSON(apiInbound))
	cfg := map[string]any{
		"log":       map[string]any{"loglevel": "warning"},
		"api":       map[string]any{"tag": "api", "services": []string{"HandlerService", "StatsService"}},
		"stats":     map[string]any{},
		"policy":    map[string]any{"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true}}, "system": map[string]any{"statsInboundUplink": true, "statsInboundDownlink": true, "statsOutboundUplink": true, "statsOutboundDownlink": true}},
		"inbounds":  in,
		"outbounds": []any{map[string]any{"protocol": "freedom", "tag": "direct"}, map[string]any{"protocol": "blackhole", "tag": "blocked"}},
		"routing":   map[string]any{"domainStrategy": "AsIs", "rules": []any{map[string]any{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"}}},
	}
	return json.MarshalIndent(cfg, "", "  ")
}
func (m *Manager) Apply(st store.State) error {
	data, err := m.Build(st)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err = os.MkdirAll(filepath.Dir(m.config), 0700); err != nil {
		return err
	}
	tmp := m.config + ".tmp"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	test := exec.CommandContext(ctx, m.binary, "run", "-test", "-config", tmp)
	out, e := test.CombinedOutput()
	if e != nil {
		_ = os.Remove(tmp)
		m.lastError = string(out)
		return fmt.Errorf("xray config validation failed: %w: %s", e, string(out))
	}
	if err = os.Rename(tmp, m.config); err != nil {
		return err
	}
	return m.restartLocked()
}
func (m *Manager) waitLocked(timeout time.Duration) error {
	if m.cmd == nil || m.done == nil {
		return nil
	}
	done := m.done
	select {
	case err := <-done:
		m.cmd = nil
		m.done = nil
		return err
	case <-time.After(timeout):
		if m.cmd != nil && m.cmd.Process != nil {
			_ = m.cmd.Process.Kill()
		}
		err := <-done
		m.cmd = nil
		m.done = nil
		return err
	}
}
func (m *Manager) restartLocked() error {
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Signal(os.Interrupt)
		_ = m.waitLocked(5 * time.Second)
	}
	cmd := exec.Command(m.binary, "run", "-config", m.config)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		m.lastError = err.Error()
		return err
	}
	m.cmd = cmd
	m.done = make(chan error, 1)
	m.started = time.Now()
	m.generation++
	m.lastError = ""
	done := m.done
	go func(c *exec.Cmd, ch chan error) {
		err := c.Wait()
		ch <- err
		m.mu.Lock()
		if m.cmd == c {
			if err != nil {
				m.lastError = err.Error()
			} else {
				m.lastError = "Xray process exited"
			}
		}
		m.mu.Unlock()
	}(cmd, done)
	return nil
}
func (m *Manager) Restart() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, e := os.Stat(m.config); errors.Is(e, os.ErrNotExist) {
		return errors.New("Xray config does not exist")
	}
	return m.restartLocked()
}
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	_ = m.cmd.Process.Signal(os.Interrupt)
	return m.waitLocked(5 * time.Second)
}
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{LastError: m.lastError}
	if m.cmd != nil && m.cmd.Process != nil && m.lastError == "" {
		s.Running = true
		s.PID = m.cmd.Process.Pid
		s.Uptime = int64(time.Since(m.started).Seconds())
	}
	return s
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func (m *Manager) QueryStats() ([]TrafficStat, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.binary, "api", "statsquery", "--server=127.0.0.1:10085", "-pattern=user>>>")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("xray stats query failed: %w", err)
	}
	var response struct {
		Stat []TrafficStat `json:"stat"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return nil, fmt.Errorf("invalid xray stats response: %w", err)
	}
	return response.Stat, nil
}

func (m *Manager) Generation() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generation
}

func (m *Manager) Supervise(ctx context.Context) {
	backoff := 5 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if m.Status().Running {
			backoff = 5 * time.Second
			continue
		}
		if _, err := os.Stat(m.config); err != nil {
			backoff = 5 * time.Second
			continue
		}
		if err := m.Restart(); err != nil {
			if backoff < time.Minute {
				backoff *= 2
				if backoff > time.Minute {
					backoff = time.Minute
				}
			}
			continue
		}
		backoff = 5 * time.Second
	}
}
