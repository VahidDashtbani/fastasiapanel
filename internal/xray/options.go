package xray

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"

	"fastasiapanel/internal/store"
)

const (
	apiPort    = 10085
	apiAddr    = "127.0.0.1:10085"
	VisionFlow = "xtls-rprx-vision"
)

var (
	protocols  = []string{"vless", "vmess", "trojan", "shadowsocks", "wireguard", "hysteria2", "mtproto"}
	networks   = []string{"tcp", "ws", "grpc", "httpupgrade", "xhttp"}
	securities = []string{"none", "tls", "reality"}
	ssMethods  = []string{"chacha20-ietf-poly1305", "aes-128-gcm", "aes-256-gcm", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"}
)

type Fallback struct {
	Name string `json:"name,omitempty"`
	Alpn string `json:"alpn,omitempty"`
	Path string `json:"path,omitempty"`
	Dest string `json:"dest"`
	Xver int    `json:"xver,omitempty"`
}

type Reality struct {
	Dest        string   `json:"dest"`
	ServerName  string   `json:"server_name"`
	ServerNames []string `json:"server_names,omitempty"`
	PrivateKey  string   `json:"private_key"`
	PublicKey   string   `json:"public_key"`
	ShortID     string   `json:"short_id"`
	ShortIDs    []string `json:"short_ids,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	SpiderX     string   `json:"spider_x,omitempty"`
}

type SniffingOpt struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"dest_override,omitempty"`
	RouteOnly    bool     `json:"route_only,omitempty"`
}

type Sockopt struct {
	TCPFastOpen         bool   `json:"tcp_fast_open,omitempty"`
	AcceptProxyProtocol bool   `json:"accept_proxy_protocol,omitempty"`
	TCPCongestion       string `json:"tcp_congestion,omitempty"`
	Mark                int    `json:"mark,omitempty"`
}

type Options struct {
	Network       string      `json:"network"`
	Security      string      `json:"security"`
	Host          string      `json:"host,omitempty"`
	Path          string      `json:"path"`
	XhttpMode     string      `json:"xhttp_mode,omitempty"`
	SNI           string      `json:"sni,omitempty"`
	ALPN          string      `json:"alpn,omitempty"`
	Fingerprint   string      `json:"fingerprint,omitempty"`
	AllowInsecure bool        `json:"allow_insecure,omitempty"`
	CertFile      string      `json:"cert_file"`
	KeyFile       string      `json:"key_file"`
	SSMethod      string      `json:"ss_method"`
	SSPassword    string      `json:"ss_password,omitempty"`
	WGPrivateKey  string      `json:"wg_private_key,omitempty"`
	WGPublicKey   string      `json:"wg_public_key,omitempty"`
	HysteriaAuth  string      `json:"hysteria_auth,omitempty"`
	MTProtoSecret string      `json:"mtproto_secret,omitempty"`
	ExternalProxy string      `json:"external_proxy,omitempty"`
	ExternalPort  int         `json:"external_port,omitempty"`
	Reality       Reality     `json:"reality"`
	Fallbacks     []Fallback  `json:"fallbacks,omitempty"`
	Sniffing      SniffingOpt `json:"sniffing"`
	Sockopt       Sockopt     `json:"sockopt"`
}

func ParseOptions(raw json.RawMessage) (Options, error) {
	o := Options{
		Sniffing: SniffingOpt{
			Enabled:      true,
			DestOverride: []string{"http", "tls", "quic", "fakedns"},
		},
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &o); err != nil {
			return o, err
		}
	}
	if o.Network == "" {
		o.Network = "tcp"
	}
	if o.Security == "" {
		o.Security = "none"
	}
	if o.Reality.Fingerprint == "" {
		o.Reality.Fingerprint = "chrome"
	}
	if o.Reality.SpiderX == "" {
		o.Reality.SpiderX = "/"
	}
	if len(o.Sniffing.DestOverride) == 0 {
		o.Sniffing.DestOverride = []string{"http", "tls", "quic"}
	}
	return o, nil
}

func Normalize(protocol string, o *Options) {
	o.Network = strings.TrimSpace(o.Network)
	o.Security = strings.TrimSpace(o.Security)
	o.Host = strings.TrimSpace(o.Host)
	o.Path = strings.TrimSpace(o.Path)
	o.XhttpMode = strings.TrimSpace(o.XhttpMode)
	o.SNI = strings.TrimSpace(o.SNI)
	o.Reality.Dest = strings.TrimSpace(o.Reality.Dest)
	o.Reality.ServerName = strings.TrimSpace(o.Reality.ServerName)
	o.Reality.ShortID = strings.ToLower(strings.TrimSpace(o.Reality.ShortID))
	if o.Reality.Fingerprint == "" {
		o.Reality.Fingerprint = "chrome"
	}
	if o.Reality.SpiderX == "" {
		o.Reality.SpiderX = "/"
	}
	if protocol == "shadowsocks" && o.SSMethod == "" {
		o.SSMethod = ssMethods[0]
	}
	if protocol != "shadowsocks" {
		o.SSMethod = ""
		o.SSPassword = ""
	}
	if (o.Network == "ws" || o.Network == "httpupgrade" || o.Network == "xhttp") && o.Path == "" {
		o.Path = "/"
	}
	if o.Network == "tcp" {
		o.Path = ""
		o.Host = ""
	}
	if o.Security != "tls" {
		o.CertFile, o.KeyFile = "", ""
	}
	if o.Security != "reality" {
		o.Reality = Reality{}
	}
	if protocol != "vless" && protocol != "trojan" {
		o.Fallbacks = nil
	}
	if o.Network == "xhttp" && o.XhttpMode == "" {
	o.XhttpMode = "auto"
}
if o.Network != "xhttp" {
	o.XhttpMode = ""
}
}

func ValidateInbound(in store.Inbound, o Options) error {
	if !slices.Contains(protocols, in.Protocol) {
		return errors.New("unsupported protocol")
	}
	if in.Port < 1 || in.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if in.Port == apiPort {
		return fmt.Errorf("port %d is reserved for the internal api", apiPort)
	}
	if in.Listen != "" && net.ParseIP(in.Listen) == nil {
		return errors.New("listen must be an ip address")
	}
	if !slices.Contains(networks, o.Network) {
		return errors.New("network must be tcp, ws, grpc, httpupgrade or xhttp")
	}
	if !slices.Contains(securities, o.Security) {
		return errors.New("security must be none, tls or reality")
	}
	switch o.Security {
	case "tls":
		if o.CertFile == "" || o.KeyFile == "" {
			return errors.New("tls requires certificate and key file paths")
		}
	case "reality":
		if in.Protocol != "vless" {
			return errors.New("reality is only available for vless")
		}
		if o.Network != "tcp" && o.Network != "grpc" && o.Network != "xhttp" {
			return errors.New("reality requires tcp, grpc or xhttp network")
		}
		if o.Reality.Dest == "" || o.Reality.ServerName == "" {
			return errors.New("reality requires dest and server name")
		}
		firstSID := strings.TrimSpace(strings.Split(o.Reality.ShortID, ",")[0])
		sid, err := hex.DecodeString(firstSID)
		if err != nil || len(sid) == 0 || len(sid) > 8 {
			return errors.New("reality short id must be 2 to 16 hex characters")
		}
	}
	if in.Protocol == "shadowsocks" {
		if o.Security != "none" || o.Network != "tcp" {
			return errors.New("shadowsocks supports only plain tcp here")
		}
		if !slices.Contains(ssMethods, o.SSMethod) {
			return errors.New("unsupported shadowsocks method")
		}
	}
	for _, f := range o.Fallbacks {
		if strings.TrimSpace(f.Dest) == "" {
			return errors.New("fallback destination cannot be empty")
		}
	}
	return nil
}

func ValidateFlow(in store.Inbound, o Options, flow string) error {
	if flow == "" {
		return nil
	}
	if flow != VisionFlow {
		return errors.New("unsupported flow")
	}
	if in.Protocol != "vless" || o.Network != "tcp" || o.Security == "none" {
		return errors.New("vision flow needs vless over tcp with tls or reality")
	}
	return nil
}

func ClientFlow(c store.Client) string {
	if c.Flow != "" {
		return c.Flow
	}
	var v struct {
		Flow string `json:"flow"`
	}
	if len(c.Config) > 0 {
		_ = json.Unmarshal(c.Config, &v)
	}
	return v.Flow
}

func EncodeClientConfig(flow string) json.RawMessage {
	if flow == "" {
		return nil
	}
	b, _ := json.Marshal(map[string]string{"flow": flow})
	return b
}

func clientSecret(c store.Client) string {
	if c.Password != "" {
		return c.Password
	}
	return c.UUID
}