package web

import (
	"encoding/json"
	"testing"

	"fastasiapanel/internal/store"
)

func TestValidateInboundRequiresMatchingRawFields(t *testing.T) {
	v := store.Inbound{Remark: "test", Protocol: "vless", Port: 443, Raw: json.RawMessage(`{"port":443,"protocol":"vless","settings":{}}`)}
	if err := validateInbound(v); err != nil {
		t.Fatal(err)
	}
	v.Port = 8443
	if err := validateInbound(v); err == nil {
		t.Fatal("mismatched port accepted")
	}
}
func TestBuildClientLinkVLESS(t *testing.T) {
	in := store.Inbound{Protocol: "vless", Port: 443, Raw: json.RawMessage(`{"protocol":"vless","port":443,"streamSettings":{"network":"ws","security":"tls","tlsSettings":{"serverName":"example.test"},"wsSettings":{"path":"/edge"}}}`)}
	c := store.Client{Email: "demo@example.test", UUID: "test-id", Config: json.RawMessage(`{"id":"test-id"}`)}
	link, err := buildClientLink(in, c, "edge.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if link == "" || link[:8] != "vless://" {
		t.Fatalf("unexpected link: %s", link)
	}
}

func TestNormalizeClientConfig(t *testing.T) {
	config, err := normalizeClientConfig("vless", "client@example.test", json.RawMessage(`{"id":"550e8400-e29b-41d4-a716-446655440000"}`))
	if err != nil {
		t.Fatal(err)
	}
	var account map[string]any
	if err = json.Unmarshal(config, &account); err != nil {
		t.Fatal(err)
	}
	if account["email"] != "client@example.test" {
		t.Fatalf("email not normalized: %s", config)
	}
	if _, err = normalizeClientConfig("vless", "client@example.test", json.RawMessage(`{"email":"x"}`)); err == nil {
		t.Fatal("missing UUID accepted")
	}
}

func TestValidateBackupRejectsDanglingClient(t *testing.T) {
	state := store.State{Inbounds: []store.Inbound{}, Clients: []store.Client{{ID: "client", InboundID: "missing", Email: "x@example.test", Config: json.RawMessage(`{"id":"id"}`)}}}
	if err := validateBackup(state); err == nil {
		t.Fatal("backup with dangling client reference accepted")
	}
}
