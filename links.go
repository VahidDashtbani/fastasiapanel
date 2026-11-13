package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
)

func buildLink(in Inbound, c Client, host string) string {
	// Reality مسیر جدا دارد
	if in.Security == "reality" {
		return buildRealityLink(in, c, host)
	}

	q := url.Values{}
	q.Set("type", in.Network)
	q.Set("security", in.Security)
	switch in.Network {
	case "ws":
		q.Set("path", in.Path)
	case "grpc":
		q.Set("serviceName", in.Path)
	}
	if in.Security == "tls" {
		q.Set("sni", host)
	}
	name := url.PathEscape(c.Email)

	switch in.Protocol {
	case "vmess":
		tls := ""
		if in.Security == "tls" {
			tls = "tls"
		}
		b, _ := json.Marshal(map[string]interface{}{
			"v": "2", "ps": c.Email, "add": host, "port": in.Port, "id": c.UUID, "aid": 0,
			"net": in.Network, "type": "none", "host": "", "path": in.Path, "tls": tls,
		})
		return "vmess://" + base64.StdEncoding.EncodeToString(b)
	case "trojan":
		return fmt.Sprintf("trojan://%s@%s:%d?%s#%s", c.UUID, host, in.Port, q.Encode(), name)
	case "shadowsocks":
		cred := base64.RawURLEncoding.EncodeToString([]byte(in.SSMethod + ":" + c.UUID))
		return fmt.Sprintf("ss://%s@%s:%d#%s", cred, host, in.Port, name)
	default: // vless
		q.Set("encryption", "none")
		if in.Network == "tcp" && in.Security == "tls" {
			q.Set("flow", "xtls-rprx-vision")
		}
		return fmt.Sprintf("vless://%s@%s:%d?%s#%s", c.UUID, host, in.Port, q.Encode(), name)
	}
}