package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Inbound struct {
	ID        string          `json:"id"`
	Tag       string          `json:"tag"`
	Remark    string          `json:"remark"`
	Protocol  string          `json:"protocol"`
	Listen    string          `json:"listen"`
	Port      int             `json:"port"`
	Enable    bool            `json:"enable"`
	Raw       json.RawMessage `json:"raw"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}
type Client struct {
	ID         string          `json:"id"`
	InboundID  string          `json:"inbound_id"`
	Email      string          `json:"email"`
	Config     json.RawMessage `json:"config,omitempty"`
	UUID       string          `json:"uuid"`
	Enable     bool            `json:"enable"`
	TotalBytes int64           `json:"total_bytes"`
	ExpiryAt   int64           `json:"expiry_at"`
	Up         int64           `json:"up"`
	Down       int64           `json:"down"`
	CreatedAt  time.Time       `json:"created_at"`
}
type State struct {
	Inbounds  []Inbound         `json:"inbounds"`
	Clients   []Client          `json:"clients"`
	Settings  map[string]string `json:"settings"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}
type Store struct {
	mu    sync.RWMutex
	path  string
	state State
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, state: State{Inbounds: []Inbound{}, Clients: []Client{}, Settings: map[string]string{}, CreatedAt: time.Now().UTC()}}
	b, e := os.ReadFile(path)
	if e == nil {
		if e = json.Unmarshal(b, &s.state); e != nil {
			return nil, e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	if s.state.Settings == nil {
		s.state.Settings = map[string]string{}
	}
	return s, nil
}
func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, _ := json.Marshal(s.state)
	var v State
	_ = json.Unmarshal(b, &v)
	return v
}
func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	var next State
	if err := json.Unmarshal(current, &next); err != nil {
		return err
	}
	if err := fn(&next); err != nil {
		return err
	}
	next.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err = os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if dir, openErr := os.Open(filepath.Dir(s.path)); openErr == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	s.state = next
	return nil
}
func NewID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}

func NewUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
func FindInbound(st *State, id string) (int, *Inbound) {
	for i := range st.Inbounds {
		if st.Inbounds[i].ID == id {
			return i, &st.Inbounds[i]
		}
	}
	return -1, nil
}
func FindClient(st *State, id string) (int, *Client) {
	for i := range st.Clients {
		if st.Clients[i].ID == id {
			return i, &st.Clients[i]
		}
	}
	return -1, nil
}
