package xray

import (
	"encoding/json"
	"sort"
	"strings"

	"fastasiapanel/internal/store"
)

func BuildConfig(st store.State) ([]byte, error) {
	logLevel := st.Settings["xray_log_level"]
	if logLevel == "" {
		logLevel = "warning"
	}
	logCfg := map[string]any{
		"loglevel": logLevel,
	}
	if st.Settings["xray_access_log"] == "true" {
		logCfg["access"] = "data/xray-access.log"
		logCfg["error"] = "data/xray-error.log"
	}

	domainStrategy := st.Settings["routing_strategy"]
	if domainStrategy == "" {
		domainStrategy = "IPIfNonMatch"
	}

	cfg := map[string]any{
		"log":   logCfg,
		"stats": map[string]any{},
		"api": map[string]any{
			"tag":      "api",
			"services": []string{"StatsService"},
		},
		"policy": map[string]any{
			"levels": map[string]any{
				"0": map[string]any{
					"statsUserUplink":   true,
					"statsUserDownlink": true,
				},
			},
			"system": map[string]any{
				"statsInboundUplink":    true,
				"statsInboundDownlink":  true,
				"statsOutboundUplink":   true,
				"statsOutboundDownlink": true,
			},
		},
		"inbounds":  buildInbounds(st),
		"outbounds": buildOutbounds(st),
		"routing": map[string]any{
			"domainStrategy": domainStrategy,
			"rules":          buildRoutingRules(st),
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func buildOutbounds(st store.State) []any {
	list := []any{
		map[string]any{
			"tag":      "direct",
			"protocol": "freedom",
		},
		map[string]any{
			"tag":      "blocked",
			"protocol": "blackhole",
		},
	}

	for _, ob := range st.Outbounds {
		if !ob.Enable {
			continue
		}
		item := map[string]any{
			"tag":      ob.Tag,
			"protocol": ob.Protocol,
		}
		switch ob.Protocol {
		case "freedom":
			item["settings"] = map[string]any{"domainStrategy": "UseIP"}
		case "blackhole":
			item["settings"] = map[string]any{"response": map[string]any{"type": "http"}}
		case "socks", "http":
			srv := map[string]any{
				"address": ob.Address,
				"port":    ob.Port,
			}
			if ob.Username != "" || ob.Password != "" {
				srv["users"] = []any{
					map[string]any{
						"user": ob.Username,
						"pass": ob.Password,
					},
				}
			}
			item["settings"] = map[string]any{
				"servers": []any{srv},
			}
		default:
			if len(ob.Settings) > 0 {
				var raw any
				if json.Unmarshal(ob.Settings, &raw) == nil {
					item["settings"] = raw
				}
			}
		}
		list = append(list, item)
	}
	return list
}

func buildRoutingRules(st store.State) []any {
	rules := []any{
		map[string]any{
			"type":        "field",
			"inboundTag":  []string{"api"},
			"outboundTag": "api",
		},
	}

	sorted := make([]store.RoutingRule, len(st.RoutingRules))
	copy(sorted, st.RoutingRules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	for _, r := range sorted {
		if !r.Enable || r.OutboundTag == "" {
			continue
		}
		item := map[string]any{
			"type":        "field",
			"outboundTag": r.OutboundTag,
		}
		if len(r.InboundTags) > 0 {
			item["inboundTag"] = r.InboundTags
		}
		if len(r.Domains) > 0 {
			item["domain"] = r.Domains
		}
		if len(r.IPs) > 0 {
			item["ip"] = r.IPs
		}
		if r.Port != "" {
			item["port"] = r.Port
		}
		if r.Network != "" {
			item["network"] = r.Network
		}
		if len(r.Protocol) > 0 {
			item["protocol"] = r.Protocol
		}
		rules = append(rules, item)
	}

	rules = append(rules, map[string]any{
		"type": "field",
		"ip": []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"127.0.0.0/8",
			"169.254.0.0/16",
			"fc00::/7",
			"fe80::/10",
			"::1/128",
		},
		"outboundTag": "blocked",
	})

	return rules
}

func buildInbounds(st store.State) []any {
	list := []any{
		map[string]any{
			"tag":      "api",
			"listen":   "127.0.0.1",
			"port":     apiPort,
			"protocol": "dokodemo-door",
			"settings": map[string]any{
				"address": "127.0.0.1",
			},
		},
	}

	for _, ib := range st.Inbounds {
		if !ib.Enable {
			continue
		}
		if ib.Protocol == "mtproto" || ib.Protocol == "hysteria2" {
			continue
		}
		o, _ := ParseOptions(ib.Raw)
		var active []store.Client
		for _, c := range st.Clients {
			if c.InboundID == ib.ID && c.Enable {
				active = append(active, c)
			}
		}

		listen := ib.Listen
		if listen == "" {
			listen = "0.0.0.0"
		}
		dests := o.Sniffing.DestOverride
		if len(dests) == 0 {
			dests = []string{"http", "tls", "quic"}
		}
		item := map[string]any{
			"tag":      ib.Tag,
			"listen":   listen,
			"port":     ib.Port,
			"protocol": ib.Protocol,
			"settings": buildProtocolSettings(ib, o, active),
			"sniffing": map[string]any{
				"enabled":      o.Sniffing.Enabled,
				"destOverride": dests,
				"routeOnly":    o.Sniffing.RouteOnly,
			},
		}
		if ib.Protocol != "wireguard" {
			item["streamSettings"] = buildStreamSettings(o)
		}
		list = append(list, item)
	}
	return list
}

func buildProtocolSettings(ib store.Inbound, o Options, active []store.Client) map[string]any {
	switch ib.Protocol {
	case "vless":
		clients := make([]any, 0, len(active))
		for _, c := range active {
			entry := map[string]any{
				"id":    c.UUID,
				"email": c.Email,
			}
			if flow := ClientFlow(c); flow != "" {
				entry["flow"] = flow
			}
			clients = append(clients, entry)
		}
		res := map[string]any{
			"clients":    clients,
			"decryption": "none",
		}
		if len(o.Fallbacks) > 0 {
			res["fallbacks"] = buildFallbacks(o.Fallbacks)
		}
		return res

	case "vmess":
		clients := make([]any, 0, len(active))
		for _, c := range active {
			clients = append(clients, map[string]any{
				"id":    c.UUID,
				"email": c.Email,
			})
		}
		return map[string]any{
			"clients": clients,
		}

	case "trojan":
		clients := make([]any, 0, len(active))
		for _, c := range active {
			pass := c.Password
			if pass == "" {
				pass = c.UUID
			}
			clients = append(clients, map[string]any{
				"password": pass,
				"email":    c.Email,
			})
		}
		res := map[string]any{
			"clients": clients,
		}
		if len(o.Fallbacks) > 0 {
			res["fallbacks"] = buildFallbacks(o.Fallbacks)
		}
		return res

	case "shadowsocks":
		method := o.SSMethod
		if method == "" {
			method = "chacha20-ietf-poly1305"
		}
		if strings.HasPrefix(method, "2022-") {
			clients := make([]any, 0, len(active))
			for _, c := range active {
				pass := c.Password
				if pass == "" {
					pass = c.UUID
				}
				clients = append(clients, map[string]any{
					"password": pass,
					"email":    c.Email,
				})
			}
			return map[string]any{
				"method":   method,
				"password": o.SSPassword,
				"clients":  clients,
				"network":  "tcp,udp",
			}
		}
		pass := o.SSPassword
		if pass == "" && len(active) > 0 {
			pass = active[0].UUID
		}
		return map[string]any{
			"method":   method,
			"password": pass,
			"network":  "tcp,udp",
		}

	case "wireguard":
		peers := make([]any, 0, len(active))
		for i, c := range active {
			peers = append(peers, map[string]any{
				"publicKey":  c.UUID,
				"allowedIPs": []string{"10.0.0." + string(rune('2'+i)) + "/32"},
			})
		}
		return map[string]any{
			"secretKey": o.WGPrivateKey,
			"peers":     peers,
		}
	}
	return map[string]any{}
}

func buildFallbacks(fbs []Fallback) []any {
	out := make([]any, 0, len(fbs))
	for _, f := range fbs {
		item := map[string]any{
			"dest": f.Dest,
		}
		if f.Name != "" {
			item["name"] = f.Name
		}
		if f.Alpn != "" {
			item["alpn"] = f.Alpn
		}
		if f.Path != "" {
			item["path"] = f.Path
		}
		if f.Xver > 0 {
			item["xver"] = f.Xver
		}
		out = append(out, item)
	}
	return out
}

func buildStreamSettings(o Options) map[string]any {
	net := o.Network
	if net == "" {
		net = "tcp"
	}
	sec := o.Security
	if sec == "" {
		sec = "none"
	}

	stream := map[string]any{
		"network":  net,
		"security": sec,
	}

	if o.Sockopt.TCPFastOpen || o.Sockopt.AcceptProxyProtocol || o.Sockopt.TCPCongestion != "" || o.Sockopt.Mark > 0 {
		so := map[string]any{}
		if o.Sockopt.TCPFastOpen {
			so["tcpFastOpen"] = true
		}
		if o.Sockopt.AcceptProxyProtocol {
			so["acceptProxyProtocol"] = true
		}
		if o.Sockopt.TCPCongestion != "" {
			so["tcpcongestion"] = o.Sockopt.TCPCongestion
		}
		if o.Sockopt.Mark > 0 {
			so["mark"] = o.Sockopt.Mark
		}
		stream["sockopt"] = so
	}

	switch net {
	case "ws":
		ws := map[string]any{}
		if o.Path != "" {
			ws["path"] = o.Path
		}
		if o.Host != "" {
			ws["headers"] = map[string]any{"Host": o.Host}
		}
		stream["wsSettings"] = ws

	case "httpupgrade":
		hu := map[string]any{}
		if o.Path != "" {
			hu["path"] = o.Path
		}
		if o.Host != "" {
			hu["host"] = o.Host
		}
		stream["httpupgradeSettings"] = hu

	case "xhttp":
		xh := map[string]any{}
		if o.Path != "" {
			xh["path"] = o.Path
		}
		if o.Host != "" {
			xh["host"] = o.Host
		}
		if o.XhttpMode != "" {
			xh["mode"] = o.XhttpMode
		}
		stream["xhttpSettings"] = xh

	case "grpc":
		grpc := map[string]any{}
		if o.Path != "" {
			grpc["serviceName"] = strings.TrimPrefix(o.Path, "/")
		}
		stream["grpcSettings"] = grpc
	}

	switch sec {
	case "tls":
		stream["tlsSettings"] = map[string]any{
			"certificates": []any{
				map[string]any{
					"certificateFile": o.CertFile,
					"keyFile":         o.KeyFile,
				},
			},
		}

	case "reality":
		r := o.Reality
		var sni []string
		for _, s := range strings.Split(r.ServerName, ",") {
			if t := strings.TrimSpace(s); t != "" {
				sni = append(sni, t)
			}
		}
		var sids []string
		for _, s := range strings.Split(r.ShortID, ",") {
			if t := strings.TrimSpace(s); t != "" {
				sids = append(sids, t)
			}
		}
		if len(sids) == 0 {
			sids = []string{""}
		}
		fp := r.Fingerprint
		if fp == "" {
			fp = "chrome"
		}
		stream["realitySettings"] = map[string]any{
			"show":        false,
			"dest":        r.Dest,
			"xver":        0,
			"serverNames": sni,
			"privateKey":  r.PrivateKey,
			"shortIds":    sids,
			"fingerprint": fp,
		}
	}

	return stream
}