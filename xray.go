package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/gorm"
)

const (
	xrayConfigPath = "./config.json"
	xrayAPIAddr    = "127.0.0.1:10085"
)

var (
	xrayCmd *exec.Cmd
	xrayMu  sync.Mutex
)

type M = map[string]interface{}

func clientEntry(in Inbound, c Client) M {
	base := M{"email": c.Email}
	if in.Protocol == "trojan" || in.Protocol == "shadowsocks" {
		base["password"] = c.UUID
	} else {
		base["id"] = c.UUID
	}
	// Vision فقط روی vless + tcp + tls/reality
	if in.Protocol == "vless" && in.Network == "tcp" &&
		(in.Security == "tls" || in.Security == "reality") {
		flow := c.Flow
		if flow == "" {
			flow = "xtls-rprx-vision"
		}
		base["flow"] = flow
	}
	return base
}

func inboundSettings(in Inbound, clients []M) M {
	switch in.Protocol {
	case "vless":
		return M{"clients": clients, "decryption": "none"}
	case "shadowsocks":
		return M{"method": in.SSMethod, "network": "tcp,udp", "clients": clients}
	default:
		return M{"clients": clients}
	}
}

func streamSettings(in Inbound) M {
	// Reality فقط روی tcp
	if in.Security == "reality" {
		return buildRealityStreamSettings(in)
	}
	s := M{"network": in.Network, "security": in.Security}
	switch in.Network {
	case "ws":
		s["wsSettings"] = M{"path": in.Path}
	case "grpc":
		s["grpcSettings"] = M{"serviceName": in.Path}
	}
	if in.Security == "tls" {
		s["tlsSettings"] = M{
			"certificates": []M{{"certificateFile": in.CertFile, "keyFile": in.KeyFile}},
			"alpn":         []string{"h2", "http/1.1"},
			"minVersion":   "1.2",
		}
	}
	return s
}

func buildXrayConfig() ([]byte, error) {
	var inbounds []Inbound
	DB.Where("enable = ?", true).Find(&inbounds)

	xIn := []M{{
		"tag": "api", "listen": "127.0.0.1", "port": 10085, "protocol": "dokodemo-door",
		"settings": M{"address": "127.0.0.1"},
	}}
	for _, in := range inbounds {
		var cs []Client
		DB.Where("inbound_id = ? AND enable = ?", in.ID, true).Find(&cs)
		list := []M{}
		for _, c := range cs {
			list = append(list, clientEntry(in, c))
		}
		xIn = append(xIn, M{
			"tag": "inbound-" + itoa(in.ID), "listen": "0.0.0.0", "port": in.Port,
			"protocol": in.Protocol, "settings": inboundSettings(in, list),
			"streamSettings": streamSettings(in),
			"sniffing":       M{"enabled": true, "destOverride": []string{"http", "tls"}},
		})
	}

	cfg := M{
		"log":   M{"loglevel": "warning"},
		"api":   M{"tag": "api", "services": []string{"HandlerService", "StatsService"}},
		"stats": M{},
		"policy": M{"levels": M{"0": M{
			"statsUserUplink":   true,
			"statsUserDownlink": true,
			"statsUserOnline":   true,
		}}},
		"routing": M{"rules": []M{{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"}}},
		"inbounds":  xIn,
		"outbounds": []M{{"protocol": "freedom", "tag": "direct"}, {"protocol": "blackhole", "tag": "blocked"}},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func itoa(n uint) string { b, _ := json.Marshal(n); return string(b) }

func startXray() error {
	data, err := buildXrayConfig()
	if err != nil {
		return err
	}
	if err := os.WriteFile(xrayConfigPath, data, 0600); err != nil {
		return err
	}
	cmd := exec.Command("xray", "-config", xrayConfigPath)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	xrayCmd = cmd
	go func() { _ = cmd.Wait(); log.Println("Xray process exited") }()
	log.Println("Xray started, PID:", cmd.Process.Pid)
	return nil
}

func stopXray() {
	if xrayCmd != nil && xrayCmd.Process != nil && xrayCmd.ProcessState == nil {
		_ = xrayCmd.Process.Kill()
		time.Sleep(300 * time.Millisecond)
	}
}

func restartXray() error {
	xrayMu.Lock()
	defer xrayMu.Unlock()
	stopXray()
	return startXray()
}

func xrayRunning() bool { return xrayCmd != nil && xrayCmd.ProcessState == nil }

// queryStats با gRPC واقعی (StatsService) آمار را می‌خواند و ریست می‌کند.
func queryStats() error {
	conn, err := grpc.NewClient(xrayAPIAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := command.NewStatsServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.QueryStats(ctx, &command.QueryStatsRequest{
		Pattern: "user>>>",
		Reset_:  true,
	})
	if err != nil {
		return err
	}

	for _, stat := range resp.Stat {
		p := strings.Split(stat.Name, ">>>")
		if len(p) != 4 || p[0] != "user" {
			continue
		}
		col := "down"
		if p[3] == "uplink" {
			col = "up"
		}
		DB.Model(&Client{}).Where("email = ?", p[1]).
			UpdateColumn(col, gorm.Expr(col+" + ?", stat.Value))
	}
	return nil
}

func statsLoop() {
	for range time.Tick(10 * time.Second) {
		if !xrayRunning() {
			continue
		}
		if err := queryStats(); err != nil {
			log.Println("stats query failed:", err)
			continue
		}
		r := DB.Model(&Client{}).
			Where("enable = ? AND ((total_bytes > 0 AND up + down >= total_bytes) OR (expiry_time > 0 AND expiry_time <= ?))",
				true, time.Now().UnixMilli()).
			Update("enable", false)
		if r.RowsAffected > 0 {
			log.Printf("disabled %d expired/over-limit clients", r.RowsAffected)
			_ = restartXray()
		}
	}
}