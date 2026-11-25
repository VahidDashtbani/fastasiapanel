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
		domainStrategy = "AsIs"
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

func enabledTags(st store.State) map[string]bool {
	t := map[string]bool{"direct": true, "blocked": true}
	for _, ob := range st.Outbounds {
		if tag := strings.TrimSpace(ob.Tag); ob.Enable && tag != "" {
			t[tag] = true
		}
	}
	return t
}

func buildOutbounds(st store.State) []any {
	tags := enabledTags(st)
	list := []any{
		map[string]any{
			"tag":      "direct",
			"protocol": "freedom",
			"settings": map[string]any{
				"domainStrategy": "AsIs",
			},
		},
		map[string]any{
			"tag":      "blocked",
			"protocol": "blackhole",
			"settings": map[string]any{
				"response": map[string]any{"type": "http"},
			},
		},
	}

	for _, ob := range st.Outbounds {
		if t := strings.TrimSpace(ob.Tag); t == "direct" || t == "blocked" || t == "api" {
			continue
		}
		if !ob.Enable || strings.TrimSpace(ob.Tag) == "" {
			continue
		}
		proto := strings.ToLower(strings.TrimSpace(ob.Protocol))
		item := map[string]any{
			"tag":      ob.Tag,
			"protocol": proto,
		}
		if rt := strings.TrimSpace(ob.RelayTag); rt != "" && rt != strings.TrimSpace(ob.Tag) && tags[rt] {
			item["proxySettings"] = map[string]any{"tag": rt, "transportLayer": true}
		}

		switch proto {
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
		case "vless":
			if len(ob.Settings) > 0 && ob.Address == "" {
				var raw any
				if json.Unmarshal(ob.Settings, &raw) == nil {
					item["settings"] = raw
				}
			} else {
				user := map[string]any{
					"id":         ob.Username,
					"encryption": "none",
				}
				if ob.Stream != nil && ob.Stream.Flow != "" {
					user["flow"] = ob.Stream.Flow
				}
				item["settings"] = map[string]any{
					"vnext": []any{
						map[string]any{
							"address": ob.Address,
							"port":    ob.Port,
							"users":   []any{user},
						},
					},
				}
			}
			if ob.Stream != nil {
				item["streamSettings"] = buildOutboundStream(ob.Stream)
			}
		case "vmess":
			if len(ob.Settings) > 0 && ob.Address == "" {
				var raw any
				if json.Unmarshal(ob.Settings, &raw) == nil {
					item["settings"] = raw
				}
			} else {
				aid := 0
				sec := "auto"
				if ob.Stream != nil {
					aid = ob.Stream.AlterID
					if ob.Stream.Cipher != "" {
						sec = ob.Stream.Cipher
					}
				}
				item["settings"] = map[string]any{
					"vnext": []any{
						map[string]any{
							"address": ob.Address,
							"port":    ob.Port,
							"users": []any{
								map[string]any{
									"id":       ob.Username,
									"alterId":  aid,
									"security": sec,
								},
							},
						},
					},
				}
			}
			if ob.Stream != nil {
				item["streamSettings"] = buildOutboundStream(ob.Stream)
			}
		case "trojan":
			if len(ob.Settings) > 0 && ob.Address == "" {
				var raw any
				if json.Unmarshal(ob.Settings, &raw) == nil {
					item["settings"] = raw
				}
			} else {
				pass := ob.Password
				if pass == "" {
					pass = ob.Username
				}
				item["settings"] = map[string]any{
					"servers": []any{
						map[string]any{
							"address":  ob.Address,
							"port":     ob.Port,
							"password": pass,
						},
					},
				}
			}
			if ob.Stream != nil {
				item["streamSettings"] = buildOutboundStream(ob.Stream)
			}
		case "shadowsocks":
			if len(ob.Settings) > 0 && ob.Address == "" {
				var raw any
				if json.Unmarshal(ob.Settings, &raw) == nil {
					item["settings"] = raw
				}
			} else {
				method := "chacha20-ietf-poly1305"
				if ob.Stream != nil && ob.Stream.Cipher != "" {
					method = ob.Stream.Cipher
				}
				item["settings"] = map[string]any{
					"servers": []any{
						map[string]any{
							"address":  ob.Address,
							"port":     ob.Port,
							"method":   method,
							"password": ob.Password,
						},
					},
				}
			}
		default:
			if len(ob.Settings) > 0 {
				var raw any
				if json.Unmarshal(ob.Settings, &raw) == nil {
					item["settings"] = raw
				}
			}
			if ob.Stream != nil {
				item["streamSettings"] = buildOutboundStream(ob.Stream)
			}
		}
		list = append(list, item)
	}
	return list
}

func buildOutboundStream(s *store.OutboundStream) map[string]any {
	netType := strings.TrimSpace(s.Network)
	if netType == "" {
		netType = "tcp"
	}
	secType := strings.TrimSpace(s.Security)
	if secType == "" {
		secType = "none"
	}
	stream := map[string]any{
		"network":  netType,
		"security": secType,
	}
	switch netType {
	case "ws":
		ws := map[string]any{}
		if s.Path != "" {
			ws["path"] = s.Path
		}
		if s.Host != "" {
			ws["headers"] = map[string]any{"Host": s.Host}
		}
		stream["wsSettings"] = ws
	case "grpc":
		grpc := map[string]any{}
		sn := s.ServiceName
		if sn == "" {
			sn = strings.TrimPrefix(s.Path, "/")
		}
		if sn != "" {
			grpc["serviceName"] = sn
		}
		stream["grpcSettings"] = grpc
	case "httpupgrade":
		hu := map[string]any{}
		if s.Path != "" {
			hu["path"] = s.Path
		}
		if s.Host != "" {
			hu["host"] = s.Host
		}
		stream["httpupgradeSettings"] = hu
	case "xhttp":
		xh := map[string]any{}
		if s.Path != "" {
			xh["path"] = s.Path
		}
		if s.Host != "" {
			xh["host"] = s.Host
		}
		if s.Mode != "" {
			xh["mode"] = s.Mode
		}
		stream["xhttpSettings"] = xh
	}
	switch secType {
	case "tls":
		tlsCfg := map[string]any{
			"allowInsecure": false,
		}
		if s.SNI != "" {
			tlsCfg["serverName"] = s.SNI
		} else if s.Host != "" {
			tlsCfg["serverName"] = s.Host
		}
		if s.Fingerprint != "" {
			tlsCfg["fingerprint"] = s.Fingerprint
		}
		if s.ALPN != "" {
			var alpns []string
			for _, a := range strings.Split(s.ALPN, ",") {
				if t := strings.TrimSpace(a); t != "" {
					alpns = append(alpns, t)
				}
			}
			if len(alpns) > 0 {
				tlsCfg["alpn"] = alpns
			}
		}
		stream["tlsSettings"] = tlsCfg
	case "reality":
		fp := s.Fingerprint
		if fp == "" {
			fp = "chrome"
		}
		spx := s.SpiderX
		if spx == "" {
			spx = "/"
		}
		stream["realitySettings"] = map[string]any{
			"serverName":  s.SNI,
			"fingerprint": fp,
			"publicKey":   s.PublicKey,
			"shortId":     s.ShortID,
			"spiderX":     spx,
		}
	}
	return stream
}

func primaryProxyOutbound(st store.State) string {
	for _, ob := range st.Outbounds {
		t := strings.TrimSpace(ob.Tag)
		if ob.Enable && t != "" && t != "direct" && t != "blocked" && t != "api" {
			return t
		}
	}
	return "direct"
}

func buildRoutingRules(st store.State) []any {
	tags := enabledTags(st)
	dnsOutbound := primaryProxyOutbound(st)
	rules := []any{
		map[string]any{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"},
		map[string]any{"type": "field", "port": "53", "outboundTag": dnsOutbound},
		map[string]any{"type": "field", "port": "443", "network": "udp", "outboundTag": "blocked"},
		map[string]any{
			"type":        "field",
			"outboundTag": "blocked",
			"ip": []string{
				"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8",
				"169.254.0.0/16", "fc00::/7", "fe80::/10", "::1/128",
			},
		},
		map[string]any{"type": "field", "outboundTag": "blocked", "protocol": []string{"bittorrent"}},
	}

	sorted := make([]store.RoutingRule, len(st.RoutingRules))
	copy(sorted, st.RoutingRules)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority < sorted[j].Priority })

	for _, r := range sorted {
		tag := strings.TrimSpace(r.OutboundTag)
		if !r.Enable || tag == "" {
			continue
		}
		if !tags[tag] {
			tag = "blocked" // fail-closed: prevent IP leak when outbound is missing/disabled
		}
		item := map[string]any{"type": "field", "outboundTag": tag}
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
		var dests []string
		for _, d := range o.Sniffing.DestOverride {
			if d != "fakedns" {
				dests = append(dests, d)
			}
		}
		if len(dests) == 0 {
			dests = []string{"http", "tls", "quic"}
		}
		sniffEnabled := true
		sniffRouteOnly := o.Sniffing.RouteOnly
		if len(ib.Sniffing) > 0 && string(ib.Sniffing) != "null" {
			var dbSniff SniffingOpt
			if json.Unmarshal(ib.Sniffing, &dbSniff) == nil {
				sniffEnabled = dbSniff.Enabled
				sniffRouteOnly = dbSniff.RouteOnly
				if len(dbSniff.DestOverride) > 0 {
					dests = nil
					for _, d := range dbSniff.DestOverride {
						if d != "fakedns" {
							dests = append(dests, d)
						}
					}
					if len(dests) == 0 {
						dests = []string{"http", "tls", "quic"}
					}
				}
			}
		} else if strings.Contains(string(ib.Raw), `"sniffing"`) {
			sniffEnabled = o.Sniffing.Enabled
		}

		item := map[string]any{
			"tag":      ib.Tag,
			"listen":   listen,
			"port":     ib.Port,
			"protocol": ib.Protocol,
			"settings": buildProtocolSettings(ib, o, active),
			"sniffing": map[string]any{
				"enabled":      sniffEnabled,
				"destOverride": dests,
				"routeOnly":    sniffRouteOnly,
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
