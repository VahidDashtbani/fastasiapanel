package xray

import (
	"strings"
	"testing"

	"fastasiapanel/internal/store"
)

func TestShadowsocksClientsCarryMethod(t *testing.T) {
	o := Options{Network: "tcp", Security: "none", SSMethod: "aes-256-gcm"}
	raw, _ := jsonMarshal(o)
	st := store.State{
		Inbounds: []store.Inbound{{ID: "i", Tag: "in-i", Protocol: "shadowsocks", Port: 8388, Enable: true, Raw: raw}},
		Clients:  []store.Client{{ID: "c", InboundID: "i", Email: "a", UUID: "secret", Enable: true}},
	}
	out, err := BuildConfig(st)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`"method": "aes-256-gcm"`, `"password": "secret"`, `"dokodemo-door"`, `"statsUserUplink": true`} {
		if !strings.Contains(s, want) {
			t.Fatalf("config is missing %s", want)
		}
	}
}

func TestDisabledClientsAreSkipped(t *testing.T) {
	st := store.State{
		Inbounds: []store.Inbound{{ID: "i", Tag: "in-i", Protocol: "vmess", Port: 1000, Enable: true}},
		Clients:  []store.Client{{ID: "c", InboundID: "i", Email: "gone", UUID: "u", Enable: false}},
	}
	out, _ := BuildConfig(st)
	if strings.Contains(string(out), "gone") {
		t.Fatal("disabled client must not be written to the config")
	}
}

func TestRoutingOrderAndFailClosed(t *testing.T) {
	st := store.State{
		Outbounds: []store.Outbound{{Tag: "nether", Protocol: "socks", Address: "127.0.0.1", Port: 40001, Enable: true}},
		RoutingRules: []store.RoutingRule{
			{ID: "a", OutboundTag: "nether", InboundTags: []string{"in-1"}, Priority: 20, Enable: true},
			{ID: "b", OutboundTag: "ghost", Domains: []string{"domain:x.com"}, Priority: 10, Enable: true},
		},
	}
	r := buildRoutingRules(st)
	if len(r) != 7 {
		t.Fatalf("want 7 rules, got %d", len(r))
	}
	if r[1].(map[string]any)["port"] != "53" || r[1].(map[string]any)["outboundTag"] != "nether" {
		t.Fatal("DNS rule must be second and route via primary proxy outbound")
	}
	if r[2].(map[string]any)["port"] != "443" || r[2].(map[string]any)["network"] != "udp" || r[2].(map[string]any)["outboundTag"] != "blocked" {
		t.Fatal("QUIC UDP 443 block must be third")
	}
	if r[3].(map[string]any)["outboundTag"] != "blocked" {
		t.Fatal("private block must precede user rules")
	}
	if r[5].(map[string]any)["outboundTag"] != "blocked" {
		t.Fatal("rule with missing outbound must fail closed")
	}
	if r[6].(map[string]any)["outboundTag"] != "nether" {
		t.Fatal("nether rule must come last")
	}
}
