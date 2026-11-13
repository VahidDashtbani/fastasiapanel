package xray

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"fastasiapanel/internal/store"
)

func TestBuildInjectsManagedClients(t *testing.T) {
	m := New("xray", "unused")
	st := store.State{Inbounds: []store.Inbound{{ID: "in-1", Tag: "in-1", Protocol: "vless", Enable: true, Raw: json.RawMessage(`{"listen":"0.0.0.0","port":443,"protocol":"vless","settings":{"decryption":"none","clients":[]}}`)}}, Clients: []store.Client{{ID: "c-1", InboundID: "in-1", Email: "a@example.test", Enable: true, Config: json.RawMessage(`{"id":"uuid","email":"a@example.test"}`)}}}
	b, err := m.Build(st)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds []struct {
			Settings struct {
				Clients []map[string]any `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if err = json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) < 2 || len(cfg.Inbounds[0].Settings.Clients) != 1 {
		t.Fatalf("client injection failed: %s", b)
	}
}

func TestBuildEnablesAPIAndUserStats(t *testing.T) {
	m := New("xray", "unused")
	b, err := m.Build(store.State{})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["api"] == nil || cfg["stats"] == nil || cfg["policy"] == nil {
		t.Fatalf("API and stats configuration missing: %s", b)
	}
	routing := cfg["routing"].(map[string]any)
	rules := routing["rules"].([]any)
	if len(rules) == 0 {
		t.Fatal("API routing rule missing")
	}
}

func TestBuildPreservesRawClients(t *testing.T) {
	m := New("xray", "unused")
	st := store.State{Inbounds: []store.Inbound{{ID: "in-1", Tag: "in-1", Protocol: "vless", Enable: true, Raw: json.RawMessage(`{"port":443,"protocol":"vless","settings":{"clients":[{"id":"manual","email":"manual"}],"decryption":"none"}}`)}}, Clients: []store.Client{{ID: "c-1", InboundID: "in-1", Email: "managed", Enable: true, Config: json.RawMessage(`{"id":"managed-id","email":"managed"}`)}}}
	b, err := m.Build(st)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds []struct {
			Settings struct {
				Clients []map[string]any `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if err = json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds[0].Settings.Clients) != 2 {
		t.Fatalf("raw client was lost: %s", b)
	}
}

func TestTrafficStatAcceptsProtobufJSONInt64(t *testing.T) {
	var stat TrafficStat
	if err := json.Unmarshal([]byte(`{"name":"user>>>demo>>>traffic>>>uplink","value":"12345"}`), &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Value != 12345 {
		t.Fatalf("unexpected value: %d", stat.Value)
	}
}

func TestQueryStatsParsesCLIResponse(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "xray")
	script := "#!/bin/sh\nprintf '%s' '{\"stat\":[{\"name\":\"user>>>demo>>>traffic>>>uplink\",\"value\":\"42\"}]}'\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m := New(bin, filepath.Join(dir, "config.json"))
	stats, err := m.QueryStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Value != 42 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}
