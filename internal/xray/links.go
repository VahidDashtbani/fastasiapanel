package xray

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"fastasiapanel/internal/store"
)

func BuildLink(in store.Inbound, c store.Client, defaultHost string) string {
	o, err := ParseOptions(in.Raw)
	if err != nil {
		return ""
	}
	targetHost := defaultHost
	if o.ExternalProxy != "" {
		targetHost = o.ExternalProxy
	}
	targetPort := in.Port
	if o.ExternalPort > 0 {
		targetPort = o.ExternalPort
	}

	bare := strings.Trim(targetHost, "[]")
	host := bare
	if strings.Contains(bare, ":") {
		host = "[" + bare + "]"
	}

	q := url.Values{}
	q.Set("type", o.Network)
	q.Set("security", o.Security)
	switch o.Network {
	case "ws", "httpupgrade":
		q.Set("path", o.Path)
		if o.Host != "" {
			q.Set("host", o.Host)
		}
	case "xhttp":
		q.Set("path", o.Path)
		if o.Host != "" {
			q.Set("host", o.Host)
		}
		if o.XhttpMode != "" {
			q.Set("mode", o.XhttpMode)
		}
	case "grpc":
		q.Set("serviceName", o.Path)
	}
	switch o.Security {
	case "tls":
		sni := bare
		if o.SNI != "" {
			sni = o.SNI
		} else if o.Host != "" {
			sni = o.Host
		}
		q.Set("sni", sni)
		if o.ALPN != "" {
			q.Set("alpn", o.ALPN)
		}
		if o.Fingerprint != "" {
			q.Set("fp", o.Fingerprint)
		}
		if o.AllowInsecure {
			q.Set("allowInsecure", "1")
		}
	case "reality":
		q.Set("pbk", o.Reality.PublicKey)
		q.Set("sni", o.Reality.ServerName)
		q.Set("sid", o.Reality.ShortID)
		fp := o.Reality.Fingerprint
		if fp == "" {
			fp = "chrome"
		}
		spx := o.Reality.SpiderX
		if spx == "" {
			spx = "/"
		}
		q.Set("fp", fp)
		q.Set("spx", spx)
	}

	remarkLabel := c.Email
	name := url.PathEscape(remarkLabel)
	secret := clientSecret(c)

	switch in.Protocol {
	case "vmess":
		tls := ""
		sni := ""
		if o.Security == "tls" {
			tls = "tls"
			sni = bare
			if o.SNI != "" {
				sni = o.SNI
			} else if o.Host != "" {
				sni = o.Host
			}
		}
		b, _ := json.Marshal(map[string]any{
			"v":    "2",
			"ps":   remarkLabel,
			"add":  bare,
			"port": targetPort,
			"id":   c.UUID,
			"aid":  0,
			"scy":  "auto",
			"net":  o.Network,
			"type": "none",
			"host": o.Host,
			"path": o.Path,
			"tls":  tls,
			"sni":  sni,
			"alpn": o.ALPN,
			"fp":   o.Fingerprint,
		})
		return "vmess://" + base64.StdEncoding.EncodeToString(b)
	case "trojan":
		return fmt.Sprintf("trojan://%s@%s:%d?%s#%s", secret, host, targetPort, q.Encode(), name)
	case "shadowsocks":
		cred := base64.RawURLEncoding.EncodeToString([]byte(o.SSMethod + ":" + secret))
		return fmt.Sprintf("ss://%s@%s:%d#%s", cred, host, targetPort, name)
	default:
		q.Set("encryption", "none")
		if f := ClientFlow(c); f != "" {
			q.Set("flow", f)
		}
		return fmt.Sprintf("vless://%s@%s:%d?%s#%s", c.UUID, host, targetPort, q.Encode(), name)
	}
}

type SubResult struct {
	Content   string
	Upload    int64
	Download  int64
	Total     int64
	ExpireSec int64
	Title     string
}

func BuildSubscription(st store.State, subID string, defaultHost string) (SubResult, bool) {
	clients := store.FindClientsBySubID(&st, subID)
	if len(clients) == 0 {
		return SubResult{}, false
	}
	inMap := make(map[string]store.Inbound, len(st.Inbounds))
	for _, in := range st.Inbounds {
		inMap[in.ID] = in
	}

	var res SubResult
	var links []string
	for _, c := range clients {
		if res.Title == "" {
			res.Title = c.Email
		}
		res.Upload += c.Up
		res.Download += c.Down
		if c.TotalBytes > 0 {
			res.Total += c.TotalBytes
		}
		if expMs := c.EffectiveExpiryAt(); expMs > 0 {
			expSec := expMs / 1000
			if res.ExpireSec == 0 || expSec > res.ExpireSec {
				res.ExpireSec = expSec
			}
		}
		in, ok := inMap[c.InboundID]
		if !ok || !in.Enable || !c.Enable {
			continue
		}
		if link := BuildLink(in, c, defaultHost); link != "" {
			links = append(links, link)
		}
	}
	joined := strings.Join(links, "\n")
	res.Content = base64.StdEncoding.EncodeToString([]byte(joined))
	return res, true
}