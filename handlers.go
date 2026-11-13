package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

func out(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, msg string) { http.Error(w, msg, code) }
func idOf(r *http.Request) uint                       { n, _ := strconv.Atoi(r.PathValue("id")); return uint(n) }
func hostOf(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		return h
	}
	return r.Host
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if !isAuthed(r) {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	data, _ := templateFS.ReadFile("templates/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	out(w, map[string]string{"name": panelName, "version": panelVersion})
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	var ni, nc int64
	DB.Model(&Inbound{}).Count(&ni)
	DB.Model(&Client{}).Count(&nc)
	out(w, M{"name": panelName, "version": panelVersion, "xray_running": xrayRunning(), "inbounds": ni, "clients": nc})
}

func validate(in *Inbound) string {
	switch {
	case in.Port < 1 || in.Port > 65535:
		return "invalid port"
	case !slices.Contains([]string{"vless", "vmess", "trojan", "shadowsocks"}, in.Protocol):
		return "protocol must be vless, vmess, trojan or shadowsocks"
	case !slices.Contains([]string{"tcp", "ws", "grpc"}, in.Network):
		return "network must be tcp, ws or grpc"
	case !slices.Contains([]string{"none", "tls", "reality"}, in.Security):
		return "security must be none, tls or reality"
	case in.Security == "tls" && (in.CertFile == "" || in.KeyFile == ""):
		return "tls needs cert_file and key_file"
	case in.Security == "reality":
		if in.Protocol != "vless" {
			return "reality only supports vless"
		}
		if in.Network != "tcp" {
			return "reality only supports tcp"
		}
		if in.RealityDest == "" || in.RealityServerName == "" {
			return "reality needs dest and server_name"
		}
		if in.RealityPrivateKey == "" || in.RealityPublicKey == "" {
			return "reality needs a key pair (use /api/reality/keys)"
		}
		if in.RealityShortID == "" || len(in.RealityShortID)%2 != 0 || len(in.RealityShortID) > 16 {
			return "reality short_id must be even-length hex, max 16 chars"
		}
	}
	if in.Protocol == "shadowsocks" && in.SSMethod == "" {
		in.SSMethod = "chacha20-ietf-poly1305"
	}
	return ""
}

func loadClients(in *Inbound, host string) {
	in.Clients = []Client{}
	DB.Where("inbound_id = ?", in.ID).Find(&in.Clients)
	for i := range in.Clients {
		in.Clients[i].Link = buildLink(*in, in.Clients[i], host)
	}
}

/* ---------- reality keys ---------- */

func realityKeysHandler(w http.ResponseWriter, r *http.Request) {
	priv, pub, err := GenerateX25519Keys()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	sid := GenShortID()
	out(w, M{"private_key": priv, "public_key": pub, "short_id": sid})
}

/* ---------- inbounds ---------- */

func listInbounds(w http.ResponseWriter, r *http.Request) {
	list := []Inbound{}
	DB.Find(&list)
	for i := range list {
		loadClients(&list[i], hostOf(r))
	}
	out(w, list)
}

func createInbound(w http.ResponseWriter, r *http.Request) {
	in := Inbound{Enable: true, Network: "tcp", Security: "none"}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		fail(w, 400, "bad json")
		return
	}
	in.ID, in.Up, in.Down = 0, 0, 0
	if m := validate(&in); m != "" {
		fail(w, 400, m)
		return
	}
	if DB.Create(&in).Error != nil {
		fail(w, 409, "port already in use")
		return
	}
	_ = restartXray()
	out(w, in)
}

func updateInbound(w http.ResponseWriter, r *http.Request) {
	var in Inbound
	if DB.First(&in, idOf(r)).Error != nil {
		fail(w, 404, "not found")
		return
	}
	id, up, down := in.ID, in.Up, in.Down
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		fail(w, 400, "bad json")
		return
	}
	in.ID, in.Up, in.Down = id, up, down
	if m := validate(&in); m != "" {
		fail(w, 400, m)
		return
	}
	if DB.Save(&in).Error != nil {
		fail(w, 409, "port already in use")
		return
	}
	_ = restartXray()
	out(w, in)
}

func toggleInbound(w http.ResponseWriter, r *http.Request) {
	var in Inbound
	if DB.First(&in, idOf(r)).Error != nil {
		fail(w, 404, "not found")
		return
	}
	DB.Model(&in).Update("enable", !in.Enable)
	_ = restartXray()
	out(w, M{"enable": !in.Enable})
}

func deleteInbound(w http.ResponseWriter, r *http.Request) {
	id := idOf(r)
	DB.Where("inbound_id = ?", id).Delete(&Client{})
	DB.Delete(&Inbound{}, id)
	_ = restartXray()
	out(w, M{"ok": true})
}

/* ---------- clients ---------- */

func listClients(w http.ResponseWriter, r *http.Request) {
	list := []Client{}
	q := DB.Model(&Client{})
	if v := r.URL.Query().Get("inbound_id"); v != "" {
		q = q.Where("inbound_id = ?", v)
	}
	q.Find(&list)
	for i := range list {
		var in Inbound
		if DB.First(&in, list[i].InboundID).Error == nil {
			list[i].Link = buildLink(in, list[i], hostOf(r))
		}
	}
	out(w, list)
}

func createClient(w http.ResponseWriter, r *http.Request) {
	c := Client{Enable: true}
	if json.NewDecoder(r.Body).Decode(&c) != nil {
		fail(w, 400, "bad json")
		return
	}
	c.ID, c.Up, c.Down, c.UUID = 0, 0, 0, uuid.NewString()
	c.Email = strings.TrimSpace(c.Email)
	if c.Email == "" {
		fail(w, 400, "email is required")
		return
	}
	var in Inbound
	if DB.First(&in, c.InboundID).Error != nil {
		fail(w, 404, "inbound not found")
		return
	}
	if DB.Create(&c).Error != nil {
		fail(w, 409, "email already exists")
		return
	}

	// gRPC: افزودن زنده
	if xrayRunning() {
		if err := AddUserLive(in, c); err != nil {
			log.Println("gRPC AddUser failed:", err)
			_ = restartXray()
		}
	}

	c.Link = buildLink(in, c, hostOf(r))
	out(w, M{"client": c, "link": c.Link})
}

func updateClient(w http.ResponseWriter, r *http.Request) {
	var c Client
	if DB.First(&c, idOf(r)).Error != nil {
		fail(w, 404, "not found")
		return
	}
	oldEmail := c.Email
	id, uid, inb, up, down := c.ID, c.UUID, c.InboundID, c.Up, c.Down
	if json.NewDecoder(r.Body).Decode(&c) != nil {
		fail(w, 400, "bad json")
		return
	}
	c.ID, c.UUID, c.InboundID, c.Up, c.Down = id, uid, inb, up, down
	if DB.Save(&c).Error != nil {
		fail(w, 409, "email already exists")
		return
	}

	var in Inbound
	DB.First(&in, c.InboundID)
	if xrayRunning() {
		if oldEmail != c.Email {
			_ = RemoveUserLive(in, oldEmail)
		}
		if err := AddUserLive(in, c); err != nil {
			log.Println("gRPC UpdateUser failed:", err)
			_ = restartXray()
		}
	}

	out(w, c)
}

func deleteClient(w http.ResponseWriter, r *http.Request) {
	var c Client
	if DB.First(&c, idOf(r)).Error != nil {
		fail(w, 404, "not found")
		return
	}
	var in Inbound
	if DB.First(&in, c.InboundID).Error == nil && xrayRunning() {
		if err := RemoveUserLive(in, c.Email); err != nil {
			log.Println("gRPC RemoveUser failed:", err)
		}
	}
	DB.Delete(&c)
	out(w, M{"ok": true})
}

func resetClientTraffic(w http.ResponseWriter, r *http.Request) {
	DB.Model(&Client{}).Where("id = ?", idOf(r)).Updates(M{"up": 0, "down": 0, "enable": true})
	out(w, M{"ok": true})
}

/* ---------- system ---------- */

func restartHandler(w http.ResponseWriter, r *http.Request) {
	if err := restartXray(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	out(w, M{"ok": true})
}

func backupHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Disposition", `attachment; filename="fastasiapanel.db"`)
	http.ServeFile(w, r, dbPath)
}