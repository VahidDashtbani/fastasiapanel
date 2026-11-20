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
