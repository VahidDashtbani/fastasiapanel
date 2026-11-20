package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

type Inbound struct {
	ID             string          `json:"id"`
	Tag            string          `json:"tag"`
	Remark         string          `json:"remark"`
	Protocol       string          `json:"protocol"`
	Listen         string          `json:"listen"`
	Port           int             `json:"port"`
	Enable         bool            `json:"enable"`
	Up             int64           `json:"up"`
	Down           int64           `json:"down"`
	TotalBytes     int64           `json:"total_bytes"`
	ExpiryAt       int64           `json:"expiry_at"`
	Settings       json.RawMessage `json:"settings,omitempty"`
	StreamSettings json.RawMessage `json:"stream_settings,omitempty"`
	Sniffing       json.RawMessage `json:"sniffing,omitempty"`
	Raw            json.RawMessage `json:"raw,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type Client struct {
	ID          string          `json:"id"`
	InboundID   string          `json:"inbound_id"`
	Email       string          `json:"email"`
	Config      json.RawMessage `json:"config,omitempty"`
	UUID        string          `json:"uuid"`
	Password    string          `json:"password,omitempty"`
	Flow        string          `json:"flow,omitempty"`
	SubID       string          `json:"sub_id"`
	TgID        string          `json:"tg_id,omitempty"`
	QuotaGroup  string          `json:"quota_group,omitempty"`
	HWID        string          `json:"hwid,omitempty"`
	Comment     string          `json:"comment,omitempty"`
	Enable      bool            `json:"enable"`
	TotalBytes  int64           `json:"total_bytes"`
	ExpiryAt    int64           `json:"expiry_at"`
	FirstSeenAt int64           `json:"first_seen_at"`
	LimitIP     int             `json:"limit_ip"`
	Reset       int             `json:"reset"`
	LastResetAt int64           `json:"last_reset_at"`
	Up          int64           `json:"up"`
	Down        int64           `json:"down"`
	CreatedAt   time.Time       `json:"created_at"`
}

type OutboundStream struct {
	Network    string `json:"network,omitempty"`     
	Security   string `json:"security,omitempty"`    
	SNI        string `json:"sni,omitempty"`
	ALPN       string `json:"alpn,omitempty"`        
	Fingerprint string `json:"fingerprint,omitempty"` 
	Path       string `json:"path,omitempty"`
	Host       string `json:"host,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
	Mode       string `json:"mode,omitempty"`        
	PublicKey  string `json:"public_key,omitempty"` 
	ShortID    string `json:"short_id,omitempty"`    
	SpiderX    string `json:"spider_x,omitempty"`    
	Flow       string `json:"flow,omitempty"`       
	AlterID    int    `json:"alter_id,omitempty"`    
	Cipher     string `json:"cipher,omitempty"`      
}

type Outbound struct {
	ID       string          `json:"id"`
	Tag      string          `json:"tag"`
	Remark   string          `json:"remark"`
	Protocol string          `json:"protocol"` 
	Address  string          `json:"address,omitempty"`
	Port     int             `json:"port,omitempty"`
	Username string          `json:"username,omitempty"` 
	Password string          `json:"password,omitempty"` 
	RelayTag string          `json:"relay_tag,omitempty"` 
	Stream   *OutboundStream `json:"stream,omitempty"`
	Settings json.RawMessage `json:"settings,omitempty"`
	Enable   bool            `json:"enable"`
}
type RoutingRule struct {
	ID          string   `json:"id"`
	Remark      string   `json:"remark"`
	OutboundTag string   `json:"outbound_tag"`
	InboundTags []string `json:"inbound_tags,omitempty"`
	Domains     []string `json:"domains,omitempty"`
	IPs         []string `json:"ips,omitempty"`
	Port        string   `json:"port,omitempty"`
	Network     string   `json:"network,omitempty"`
	Protocol    []string `json:"protocol,omitempty"`
	Priority    int      `json:"priority"`
	Enable      bool     `json:"enable"`
}

type QuotaGroup struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TotalBytes int64  `json:"total_bytes"`
	Enable     bool   `json:"enable"`
}

type Node struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Token   string `json:"token"`
	Status  string `json:"status"`
}

type APIToken struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Token     string `json:"token"`
	Scope     string `json:"scope"`
	ExpiresAt int64  `json:"expires_at"`
}

func (c *Client) UsedBytes() int64 {
	return c.Up + c.Down
}

func (c *Client) IsTrafficExceeded() bool {
	return c.TotalBytes > 0 && c.UsedBytes() >= c.TotalBytes
}

func (c *Client) EffectiveExpiryAt() int64 {
	if c.ExpiryAt > 0 {
		return c.ExpiryAt
	}
	if c.ExpiryAt < 0 && c.FirstSeenAt > 0 {
		return c.FirstSeenAt + (-c.ExpiryAt)
	}
	return 0
}

func (c *Client) IsExpired(nowMs int64) bool {
	exp := c.EffectiveExpiryAt()
	return exp > 0 && nowMs >= exp
}

func (c *Client) ShouldDisable(nowMs int64) bool {
	return c.IsTrafficExceeded() || c.IsExpired(nowMs)
}

type State struct {
	Inbounds     []Inbound         `json:"inbounds"`
	Clients      []Client          `json:"clients"`
	Outbounds    []Outbound        `json:"outbounds"`
	RoutingRules []RoutingRule     `json:"routing_rules"`
	QuotaGroups  []QuotaGroup      `json:"quota_groups"`
	Nodes        []Node            `json:"nodes"`
	APITokens    []APIToken        `json:"api_tokens"`
	Settings     map[string]string `json:"settings"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type Backend interface {
	Load() (State, error)
	Save(prev, next State) error
	Close() error
}

type Store struct {
	mu      sync.RWMutex
	backend Backend
	state   State
}

func normalize(st *State) {
	if st.Inbounds == nil {
		st.Inbounds = []Inbound{}
	}
	if st.Clients == nil {
		st.Clients = []Client{}
	}
	if st.Outbounds == nil {
		st.Outbounds = []Outbound{}
	}
	if st.RoutingRules == nil {
		st.RoutingRules = []RoutingRule{}
	}
	if st.QuotaGroups == nil {
		st.QuotaGroups = []QuotaGroup{}
	}
	if st.Nodes == nil {
		st.Nodes = []Node{}
	}
	if st.APITokens == nil {
		st.APITokens = []APIToken{}
	}
	if st.Settings == nil {
		st.Settings = map[string]string{}
	}
}

func clone(st State) (State, error) {
	raw, err := json.Marshal(st)
	if err != nil {
		return State{}, err
	}
	var out State
	if err = json.Unmarshal(raw, &out); err != nil {
		return State{}, err
	}
	normalize(&out)
	return out, nil
}

func New(b Backend) (*Store, error) {
	st, err := b.Load()
	if err != nil {
		return nil, err
	}
	normalize(&st)
	if st.CreatedAt.IsZero() {
		st.CreatedAt = time.Now().UTC()
	}
	return &Store{backend: b, state: st}, nil
}

func Open(path string) (*Store, error) {
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		return New(&jsonBackend{path: path})
	}
	return OpenSQL("sqlite", SQLiteDSN(path))
}

func (s *Store) Close() error {
	return s.backend.Close()
}

func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, _ := clone(s.state)
	return v
}

func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := clone(s.state)
	if err != nil {
		return err
	}
	if err = fn(&next); err != nil {
		return err
	}
	normalize(&next)
	next.UpdatedAt = time.Now().UTC()
	if err = s.backend.Save(s.state, next); err != nil {
		return err
	}
	s.state = next
	return nil
}

func (s *Store) Setting(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Settings[key]
}

func (s *Store) Replace(st State) error {
	return s.Update(func(cur *State) error {
		*cur = st
		return nil
	})
}

func NewID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}

func NewSubID() (string, error) {
	b := make([]byte, 8)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}

func NewToken() (string, error) {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return "fap_" + hex.EncodeToString(b), nil
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

func FindClientByEmail(st *State, email string) (int, *Client) {
	for i := range st.Clients {
		if st.Clients[i].Email == email {
			return i, &st.Clients[i]
		}
	}
	return -1, nil
}

func FindClientsBySubID(st *State, subID string) []Client {
	if subID == "" {
		return nil
	}
	var out []Client
	for _, c := range st.Clients {
		if c.SubID == subID {
			out = append(out, c)
		}
	}
	return out
}

func GroupUsedBytes(st *State, groupID string) int64 {
	if groupID == "" {
		return 0
	}
	var total int64
	for _, c := range st.Clients {
		if c.QuotaGroup == groupID {
			total += c.UsedBytes()
		}
	}
	return total
}

func EnforceQuotaGroups(st *State) bool {
	changed := false
	for _, g := range st.QuotaGroups {
		if !g.Enable || g.TotalBytes <= 0 {
			continue
		}
		if GroupUsedBytes(st, g.ID) >= g.TotalBytes {
			for i := range st.Clients {
				if st.Clients[i].QuotaGroup == g.ID && st.Clients[i].Enable {
					st.Clients[i].Enable = false
					changed = true
				}
			}
		}
	}
	return changed
}

func ValidateAPIToken(st *State, rawToken, requiredScope string, nowMs int64) bool {
	if rawToken == "" {
		return false
	}
	for _, t := range st.APITokens {
		if t.Token != rawToken {
			continue
		}
		if t.ExpiresAt > 0 && nowMs >= t.ExpiresAt {
			return false
		}
		if t.Scope == "*" || requiredScope == "" {
			return true
		}
		for _, s := range strings.Split(t.Scope, ",") {
			if strings.TrimSpace(s) == requiredScope {
				return true
			}
		}
		return false
	}
	return false
}