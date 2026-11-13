package web

import (
	"archive/zip"
	"bytes"
	"crypto/ecdh"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"fastasiapanel/internal/auth"
	"fastasiapanel/internal/config"
	"fastasiapanel/internal/store"
	"fastasiapanel/internal/xray"
)

type App struct {
	db   *store.Store
	xray *xray.Manager
	auth *auth.Auth
	cfg  config.Config
}

func New(db *store.Store, x *xray.Manager, a *auth.Auth, c config.Config) *App {
	return &App{db: db, xray: x, auth: a, cfg: c}
}
func (a *App) Register(m *http.ServeMux) {
	p := a.cfg.BasePath
	if p != "" {
		m.HandleFunc("GET "+p, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, p+"/", http.StatusPermanentRedirect) })
	}
	handle := func(method, path string, h http.HandlerFunc) { m.HandleFunc(method+" "+p+path, h) }
	handle("GET", "/", a.index)
	handle("GET", "/assets.css", a.assets)
	handle("GET", "/app.js", a.assets)
	handle("GET", "/login", a.loginPage)
	handle("POST", "/api/login", a.login)
	handle("POST", "/api/logout", a.protected(a.logout))
	handle("POST", "/api/password", a.protected(a.changePassword))
	handle("GET", "/api/status", a.protected(a.status))
	handle("GET", "/api/inbounds", a.protected(a.listInbounds))
	handle("POST", "/api/inbounds", a.protected(a.createInbound))
	handle("PUT", "/api/inbounds/{id}", a.protected(a.updateInbound))
	handle("POST", "/api/inbounds/{id}/toggle", a.protected(a.toggleInbound))
	handle("DELETE", "/api/inbounds/{id}", a.protected(a.deleteInbound))
	handle("GET", "/api/clients", a.protected(a.listClients))
	handle("POST", "/api/clients", a.protected(a.createClient))
	handle("PUT", "/api/clients/{id}", a.protected(a.updateClient))
	handle("POST", "/api/clients/{id}/toggle", a.protected(a.toggleClient))
	handle("DELETE", "/api/clients/{id}", a.protected(a.deleteClient))
	handle("POST", "/api/clients/{id}/reset", a.protected(a.resetClient))
	handle("POST", "/api/xray/restart", a.protected(a.restartXray))
	handle("GET", "/api/backup", a.protected(a.backup))
	handle("POST", "/api/backup/restore", a.protected(a.restoreBackup))
	handle("GET", "/sub/{id}", a.subscription)
}
func (a *App) protected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.auth.Valid(r) {
			jsonError(w, 401, "unauthorized")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, e := neturlHost(origin)
				if e != nil || u != r.Host {
					jsonError(w, 403, "origin rejected")
					return
				}
			}
		}
		next(w, r)
	}
}
func neturlHost(v string) (string, error) {
	i := strings.Index(v, "://")
	if i < 0 {
		return "", errors.New("invalid origin")
	}
	return strings.TrimRight(v[i+3:], "/"), nil
}
func (a *App) index(w http.ResponseWriter, r *http.Request) {
	if !a.auth.Valid(r) {
		http.Redirect(w, r, a.cfg.BasePath+"/login", http.StatusFound)
		return
	}
	a.page(w, "index.html")
}
func (a *App) loginPage(w http.ResponseWriter, r *http.Request) { a.page(w, "login.html") }
func (a *App) assets(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, a.cfg.BasePath+"/")
	if name != "assets.css" && name != "app.js" {
		http.NotFound(w, r)
		return
	}
	contentType := "text/javascript; charset=utf-8"
	if name == "assets.css" {
		contentType = "text/css; charset=utf-8"
	}
	b, e := os.ReadFile("web/" + name)
	if e != nil {
		http.Error(w, "asset unavailable", 404)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write(b)
}
func (a *App) page(w http.ResponseWriter, name string) {
	b, e := os.ReadFile("web/" + name)
	if e != nil {
		http.Error(w, "web assets unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !a.auth.LoginAllowed(ip) {
		jsonError(w, 429, "too many attempts")
		return
	}
	var q struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decodeErr := decode(r, &q)
	passwordOK := false
	storedHash := a.db.Snapshot().Settings["admin_password_hash"]
	if storedHash != "" {
		passwordOK = auth.VerifyPassword(q.Password, storedHash)
	} else {
		passwordOK = subtle.ConstantTimeCompare([]byte(q.Password), []byte(a.cfg.AdminPassword)) == 1
	}
	if decodeErr != nil || q.Username != a.cfg.AdminUser || !passwordOK {
		a.auth.Failed(ip)
		jsonError(w, 401, "invalid credentials")
		return
	}
	a.auth.Succeeded(ip)
	a.auth.Set(w, q.Username)
	jsonOut(w, map[string]bool{"ok": true})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	a.auth.Clear(w)
	jsonOut(w, map[string]bool{"ok": true})
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Current string `json:"current_password"`
		Next    string `json:"new_password"`
	}
	if decode(r, &input) != nil || len(input.Next) < 12 || len(input.Next) > 256 {
		jsonError(w, 400, "new password must contain 12 to 256 characters")
		return
	}
	storedHash := a.db.Snapshot().Settings["admin_password_hash"]
	valid := false
	if storedHash != "" {
		valid = auth.VerifyPassword(input.Current, storedHash)
	} else {
		valid = subtle.ConstantTimeCompare([]byte(input.Current), []byte(a.cfg.AdminPassword)) == 1
	}
	if !valid {
		jsonError(w, 403, "current password is incorrect")
		return
	}
	hash, err := auth.HashPassword(input.Next)
	if err != nil {
		jsonError(w, 500, "password hashing failed")
		return
	}
	if err = a.db.Update(func(s *store.State) error { s.Settings["admin_password_hash"] = hash; return nil }); err != nil {
		jsonError(w, 500, "password update failed")
		return
	}
	jsonOut(w, map[string]bool{"ok": true})
}

func (a *App) status(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s := a.db.Snapshot()
	up := 0
	for _, c := range s.Clients {
		if c.Enable && (c.ExpiryAt == 0 || c.ExpiryAt > time.Now().UnixMilli()) && (c.TotalBytes == 0 || c.Up+c.Down < c.TotalBytes) {
			up++
		}
	}
	jsonOut(w, map[string]any{"name": "FastAsiaPanel", "version": "1.1.0-dev", "xray": a.xray.Status(), "inbounds": len(s.Inbounds), "clients": len(s.Clients), "active_clients": up, "memory_alloc": ms.Alloc, "goroutines": runtime.NumGoroutine(), "platform": runtime.GOOS, "arch": runtime.GOARCH, "updated_at": s.UpdatedAt})
}
func (a *App) listInbounds(w http.ResponseWriter, r *http.Request) {
	jsonOut(w, a.db.Snapshot().Inbounds)
}
func (a *App) createInbound(w http.ResponseWriter, r *http.Request) {
	var v store.Inbound
	if decode(r, &v) != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if e := validateInbound(v); e != nil {
		jsonError(w, 400, e.Error())
		return
	}
	id, e := store.NewID()
	if e != nil {
		jsonError(w, 500, "id generation failed")
		return
	}
	v.ID = id
	v.Tag = "in-" + id[:12]
	v.Enable = true
	v.CreatedAt = time.Now().UTC()
	v.UpdatedAt = v.CreatedAt
	if e = a.db.Update(func(s *store.State) error {
		for _, old := range s.Inbounds {
			if old.Port == v.Port {
				return errors.New("port already exists")
			}
		}
		s.Inbounds = append(s.Inbounds, v)
		return nil
	}); e != nil {
		jsonError(w, 409, e.Error())
		return
	}
	if e = a.xray.Apply(a.db.Snapshot()); e != nil {
		jsonError(w, 500, e.Error())
		return
	}
	jsonOut(w, v)
}
func (a *App) updateInbound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var v store.Inbound
	if decode(r, &v) != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if e := validateInbound(v); e != nil {
		jsonError(w, 400, e.Error())
		return
	}
	e := a.db.Update(func(s *store.State) error {
		i, old := store.FindInbound(s, id)
		if old == nil {
			return errors.New("not found")
		}
		for _, x := range s.Inbounds {
			if x.ID != id && x.Port == v.Port {
				return errors.New("port already exists")
			}
		}
		v.ID = id
		v.Tag = old.Tag
		v.CreatedAt = old.CreatedAt
		v.UpdatedAt = time.Now().UTC()
		s.Inbounds[i] = v
		return nil
	})
	if e != nil {
		jsonError(w, 400, e.Error())
		return
	}
	if e = a.xray.Apply(a.db.Snapshot()); e != nil {
		jsonError(w, 500, e.Error())
		return
	}
	jsonOut(w, v)
}

func (a *App) toggleInbound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Enable *bool `json:"enable"`
	}
	if decode(r, &input) != nil || input.Enable == nil {
		jsonError(w, 400, "enable must be provided")
		return
	}
	err := a.db.Update(func(s *store.State) error {
		_, in := store.FindInbound(s, id)
		if in == nil {
			return errors.New("not found")
		}
		in.Enable = *input.Enable
		in.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		jsonError(w, 404, err.Error())
		return
	}
	if err = a.xray.Apply(a.db.Snapshot()); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOut(w, map[string]bool{"ok": true})
}

func (a *App) deleteInbound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	e := a.db.Update(func(s *store.State) error {
		i, _ := store.FindInbound(s, id)
		if i < 0 {
			return errors.New("not found")
		}
		s.Inbounds = append(s.Inbounds[:i], s.Inbounds[i+1:]...)
		out := s.Clients[:0]
		for _, c := range s.Clients {
			if c.InboundID != id {
				out = append(out, c)
			}
		}
		s.Clients = out
		return nil
	})
	if e != nil {
		jsonError(w, 404, e.Error())
		return
	}
	if e = a.xray.Apply(a.db.Snapshot()); e != nil {
		jsonError(w, 500, e.Error())
		return
	}
	jsonOut(w, map[string]bool{"ok": true})
}
func (a *App) listClients(w http.ResponseWriter, r *http.Request) {
	s := a.db.Snapshot()
	id := r.URL.Query().Get("inbound_id")
	if id != "" {
		out := []store.Client{}
		for _, c := range s.Clients {
			if c.InboundID == id {
				out = append(out, c)
			}
		}
		jsonOut(w, out)
		return
	}
	jsonOut(w, s.Clients)
}
func (a *App) createClient(w http.ResponseWriter, r *http.Request) {
	var c store.Client
	if decode(r, &c) != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	c.Email = strings.TrimSpace(c.Email)
	if c.Email == "" || len(c.Email) > 128 {
		jsonError(w, 400, "email is required and must be at most 128 characters")
		return
	}
	id, e := store.NewID()
	if e != nil {
		jsonError(w, 500, "id generation failed")
		return
	}
	uuid, e := store.NewUUID()
	if e != nil {
		jsonError(w, 500, "credential generation failed")
		return
	}
	if c.TotalBytes < 0 || c.ExpiryAt < 0 {
		jsonError(w, 400, "traffic limit and expiry cannot be negative")
		return
	}
	c.ID = id
	c.UUID = uuid
	c.Enable = true
	c.CreatedAt = time.Now().UTC()
	e = a.db.Update(func(s *store.State) error {
		_, in := store.FindInbound(s, c.InboundID)
		if in == nil {
			return errors.New("inbound not found")
		}
		if !supportsManagedClients(in.Protocol) {
			return errors.New("managed clients are not available for this protocol; configure accounts in the raw Xray inbound JSON")
		}
		for _, old := range s.Clients {
			if old.Email == c.Email {
				return errors.New("email already exists")
			}
		}
		if len(c.Config) == 0 {
			c.Config, _ = defaultClientConfig(in.Protocol, c.Email, c.UUID)
		}
		var configErr error
		c.Config, configErr = normalizeClientConfig(in.Protocol, c.Email, c.Config)
		if configErr != nil {
			return configErr
		}
		s.Clients = append(s.Clients, c)
		return nil
	})
	if e != nil {
		jsonError(w, 400, e.Error())
		return
	}
	if e = a.xray.Apply(a.db.Snapshot()); e != nil {
		jsonError(w, 500, e.Error())
		return
	}
	jsonOut(w, map[string]any{"client": c, "subscription": a.subscriptionURL(r, c.ID)})
}
func (a *App) updateClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input store.Client
	if decode(r, &input) != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	e := a.db.Update(func(s *store.State) error {
		i, c := store.FindClient(s, id)
		if c == nil {
			return errors.New("not found")
		}
		input.Email = strings.TrimSpace(input.Email)
		if input.Email == "" || len(input.Email) > 128 {
			return errors.New("email is required and must be at most 128 characters")
		}
		if input.TotalBytes < 0 || input.ExpiryAt < 0 {
			return errors.New("traffic limit and expiry cannot be negative")
		}
		for _, other := range s.Clients {
			if other.ID != id && other.Email == input.Email {
				return errors.New("email already exists")
			}
		}
		if len(input.Config) == 0 {
			input.Config = c.Config
		}
		_, inbound := store.FindInbound(s, c.InboundID)
		if inbound == nil {
			return errors.New("inbound not found")
		}
		var configErr error
		input.Config, configErr = normalizeClientConfig(inbound.Protocol, input.Email, input.Config)
		if configErr != nil {
			return configErr
		}
		input.ID = id
		input.UUID = c.UUID
		input.InboundID = c.InboundID
		input.Up = c.Up
		input.Down = c.Down
		input.CreatedAt = c.CreatedAt
		s.Clients[i] = input
		return nil
	})
	if e != nil {
		jsonError(w, 404, e.Error())
		return
	}
	if e = a.xray.Apply(a.db.Snapshot()); e != nil {
		jsonError(w, 500, e.Error())
		return
	}
	jsonOut(w, map[string]bool{"ok": true})
}

func (a *App) toggleClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var enabled struct {
		Enable *bool `json:"enable"`
	}
	if decode(r, &enabled) != nil || enabled.Enable == nil {
		jsonError(w, 400, "enable must be provided")
		return
	}
	err := a.db.Update(func(s *store.State) error {
		_, c := store.FindClient(s, id)
		if c == nil {
			return errors.New("not found")
		}
		if *enabled.Enable && ((c.ExpiryAt > 0 && c.ExpiryAt <= time.Now().UnixMilli()) || (c.TotalBytes > 0 && c.Up+c.Down >= c.TotalBytes)) {
			return errors.New("client is expired or has reached its traffic limit")
		}
		c.Enable = *enabled.Enable
		return nil
	})
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if err = a.xray.Apply(a.db.Snapshot()); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOut(w, map[string]bool{"ok": true})
}

func (a *App) deleteClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	e := a.db.Update(func(s *store.State) error {
		i, _ := store.FindClient(s, id)
		if i < 0 {
			return errors.New("not found")
		}
		s.Clients = append(s.Clients[:i], s.Clients[i+1:]...)
		return nil
	})
	if e != nil {
		jsonError(w, 404, e.Error())
		return
	}
	if e = a.xray.Apply(a.db.Snapshot()); e != nil {
		jsonError(w, 500, e.Error())
		return
	}
	jsonOut(w, map[string]bool{"ok": true})
}
func (a *App) resetClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	e := a.db.Update(func(s *store.State) error {
		_, c := store.FindClient(s, id)
		if c == nil {
			return errors.New("not found")
		}
		c.Up = 0
		c.Down = 0
		c.Enable = c.ExpiryAt == 0 || c.ExpiryAt > time.Now().UnixMilli()
		return nil
	})
	if e != nil {
		jsonError(w, 404, e.Error())
		return
	}
	if e = a.xray.Apply(a.db.Snapshot()); e != nil {
		jsonError(w, 500, e.Error())
		return
	}
	jsonOut(w, map[string]bool{"ok": true})
}
func (a *App) restartXray(w http.ResponseWriter, r *http.Request) {
	if e := a.xray.Restart(); e != nil {
		jsonError(w, 500, e.Error())
		return
	}
	jsonOut(w, map[string]bool{"ok": true})
}
func (a *App) backup(w http.ResponseWriter, r *http.Request) {
	s := a.db.Snapshot()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	f, e := z.Create("fastasiapanel-backup.json")
	if e != nil {
		jsonError(w, 500, "backup failed")
		return
	}
	_ = json.NewEncoder(f).Encode(s)
	_ = z.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=fastasiapanel-backup.zip")
	w.Write(b.Bytes())
}

func (a *App) restoreBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, 400, "backup upload is too large or unreadable")
		return
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		jsonError(w, 400, "invalid backup archive")
		return
	}
	var restored *store.State
	for _, file := range archive.File {
		if file.Name != "fastasiapanel-backup.json" {
			continue
		}
		if file.UncompressedSize64 > 20<<20 {
			jsonError(w, 400, "backup data is too large")
			return
		}
		reader, openErr := file.Open()
		if openErr != nil {
			jsonError(w, 400, "backup data cannot be opened")
			return
		}
		var state store.State
		decoder := json.NewDecoder(io.LimitReader(reader, 20<<20))
		decoder.DisallowUnknownFields()
		decodeErr := decoder.Decode(&state)
		closeErr := reader.Close()
		if decodeErr != nil || closeErr != nil {
			jsonError(w, 400, "backup JSON is invalid")
			return
		}
		restored = &state
		break
	}
	if restored == nil {
		jsonError(w, 400, "backup JSON file is missing")
		return
	}
	if err = validateBackup(*restored); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	old := a.db.Snapshot()
	if err = a.db.Update(func(current *store.State) error {
		adminHash := current.Settings["admin_password_hash"]
		*current = *restored
		if current.Settings == nil {
			current.Settings = map[string]string{}
		}
		if adminHash != "" {
			current.Settings["admin_password_hash"] = adminHash
		} else {
			delete(current.Settings, "admin_password_hash")
		}
		return nil
	}); err != nil {
		jsonError(w, 500, "backup could not be saved")
		return
	}
	if err = a.xray.Apply(a.db.Snapshot()); err != nil {
		_ = a.db.Update(func(current *store.State) error { *current = old; return nil })
		_ = a.xray.Apply(old)
		jsonError(w, 500, "Xray rejected restored configuration; previous data was restored")
		return
	}
	jsonOut(w, map[string]bool{"ok": true})
}

func validateBackup(state store.State) error {
	if len(state.Inbounds) > 1000 || len(state.Clients) > 100000 {
		return errors.New("backup contains too many records")
	}
	inboundIDs := map[string]bool{}
	ports := map[int]bool{}
	for _, in := range state.Inbounds {
		if in.ID == "" || inboundIDs[in.ID] {
			return errors.New("backup has duplicate or empty inbound IDs")
		}
		if err := validateInbound(in); err != nil {
			return fmt.Errorf("invalid inbound %s: %w", in.Remark, err)
		}
		if ports[in.Port] {
			return errors.New("backup has duplicate inbound ports")
		}
		inboundIDs[in.ID] = true
		ports[in.Port] = true
	}
	clientIDs := map[string]bool{}
	emails := map[string]bool{}
	for _, client := range state.Clients {
		if client.ID == "" || clientIDs[client.ID] {
			return errors.New("backup has duplicate or empty client IDs")
		}
		if !inboundIDs[client.InboundID] {
			return errors.New("backup client references a missing inbound")
		}
		if client.Email == "" || emails[client.Email] {
			return errors.New("backup has duplicate or empty client emails")
		}
		if client.TotalBytes < 0 || client.ExpiryAt < 0 || client.Up < 0 || client.Down < 0 || !json.Valid(client.Config) {
			return errors.New("backup contains invalid client data")
		}
		clientIDs[client.ID] = true
		emails[client.Email] = true
	}
	return nil
}

func (a *App) subscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s := a.db.Snapshot()
	var c *store.Client
	for i := range s.Clients {
		if s.Clients[i].ID == id {
			c = &s.Clients[i]
			break
		}
	}
	if c == nil || !c.Enable || (c.ExpiryAt > 0 && c.ExpiryAt <= time.Now().UnixMilli()) || (c.TotalBytes > 0 && c.Up+c.Down >= c.TotalBytes) {
		http.NotFound(w, r)
		return
	}
	var lines []string
	for _, in := range s.Inbounds {
		if in.ID == c.InboundID && in.Enable {
			host := a.cfg.PublicHost
			if host == "" {
				host = r.Host
				if h, _, splitErr := net.SplitHostPort(host); splitErr == nil {
					host = h
				}
			}
			line, err := buildClientLink(in, *c, host)
			if err != nil {
				jsonError(w, 422, err.Error())
				return
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		http.NotFound(w, r)
		return
	}
	payload := base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", c.Up, c.Down, c.TotalBytes, c.ExpiryAt/1000))
	io.WriteString(w, payload)
}
func (a *App) subscriptionURL(r *http.Request, id string) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host + a.cfg.BasePath + "/sub/" + id
}
func validateInbound(v store.Inbound) error {
	if v.Remark == "" || len(v.Remark) > 80 {
		return errors.New("remark is required and limited to 80 characters")
	}
	if v.Port < 1 || v.Port > 65535 || v.Port == 10085 {
		return errors.New("invalid or reserved port")
	}
	if v.Protocol == "" || !json.Valid(v.Raw) {
		return errors.New("protocol and valid raw Xray inbound JSON are required")
	}
	var raw map[string]any
	if json.Unmarshal(v.Raw, &raw) != nil {
		return errors.New("raw inbound must be a JSON object")
	}
	if raw["protocol"] != v.Protocol {
		return errors.New("protocol does not match raw inbound JSON")
	}
	if raw["tag"] == "api" {
		return errors.New("inbound tag api is reserved")
	}
	if _, ok := raw["settings"].(map[string]any); !ok {
		return errors.New("inbound settings must be a JSON object")
	}
	if p, ok := raw["port"].(float64); !ok || int(p) != v.Port {
		return errors.New("port does not match raw inbound JSON")
	}
	return nil
}
func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func jsonOut(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
func supportsManagedClients(protocol string) bool {
	switch protocol {
	case "vless", "vmess", "trojan", "shadowsocks", "socks", "http":
		return true
	}
	return false
}
func defaultClientConfig(protocol, email, id string) (json.RawMessage, error) {
	v := map[string]any{"email": email}
	switch protocol {
	case "vless", "vmess":
		v["id"] = id
	case "trojan":
		v["password"] = id
	case "shadowsocks":
		v["password"] = id
		v["method"] = "chacha20-ietf-poly1305"
	case "socks", "http":
		v["user"] = email
		v["pass"] = id
	default:
		return nil, errors.New("unsupported managed client protocol")
	}
	return json.Marshal(v)
}

func normalizeClientConfig(protocol, email string, raw json.RawMessage) (json.RawMessage, error) {
	var account map[string]any
	if !json.Valid(raw) || json.Unmarshal(raw, &account) != nil || account == nil {
		return nil, errors.New("client config must be a JSON object")
	}
	required := []string{}
	switch protocol {
	case "vless", "vmess":
		required = []string{"id"}
	case "trojan":
		required = []string{"password"}
	case "shadowsocks":
		required = []string{"password", "method"}
	case "socks", "http":
		required = []string{"user", "pass"}
	default:
		return nil, errors.New("unsupported managed client protocol")
	}
	for _, key := range required {
		value, ok := account[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("client config requires a non-empty %s", key)
		}
	}
	if protocol == "vless" || protocol == "vmess" || protocol == "trojan" || protocol == "shadowsocks" {
		account["email"] = email
	}
	return json.Marshal(account)
}

func buildClientLink(in store.Inbound, c store.Client, host string) (string, error) {
	var account map[string]any
	if err := json.Unmarshal(c.Config, &account); err != nil {
		return "", err
	}
	credential := c.UUID
	if v, ok := account["id"].(string); ok && v != "" {
		credential = v
	}
	if v, ok := account["password"].(string); ok && v != "" {
		credential = v
	}
	name := url.PathEscape(c.Email)
	q := url.Values{}
	var root map[string]any
	if err := json.Unmarshal(in.Raw, &root); err != nil {
		return "", err
	}
	ss, _ := root["streamSettings"].(map[string]any)
	network, _ := ss["network"].(string)
	security, _ := ss["security"].(string)
	if network == "" {
		network = "tcp"
	}
	q.Set("type", network)
	q.Set("security", security)
	if tls, ok := ss["tlsSettings"].(map[string]any); ok {
		if sni, ok := tls["serverName"].(string); ok && sni != "" {
			q.Set("sni", sni)
		}
	}
	if network == "ws" {
		if v, ok := ss["wsSettings"].(map[string]any); ok {
			if p, ok := v["path"].(string); ok {
				q.Set("path", p)
			}
			if h, ok := v["host"].(string); ok && h != "" {
				q.Set("host", h)
			}
		}
	}
	if network == "grpc" {
		if v, ok := ss["grpcSettings"].(map[string]any); ok {
			if p, ok := v["serviceName"].(string); ok {
				q.Set("serviceName", p)
			}
			if v["multiMode"] == true {
				q.Set("mode", "multi")
			}
		}
	}
	if network == "xhttp" {
		if v, ok := ss["xhttpSettings"].(map[string]any); ok {
			if p, ok := v["path"].(string); ok {
				q.Set("path", p)
			}
			if m, ok := v["mode"].(string); ok {
				q.Set("mode", m)
			}
		}
	}
	if security == "reality" {
		if v, ok := ss["realitySettings"].(map[string]any); ok {
			if names, ok := v["serverNames"].([]any); ok && len(names) > 0 {
				if sni, ok := names[0].(string); ok {
					q.Set("sni", sni)
				}
			}
			if ids, ok := v["shortIds"].([]any); ok && len(ids) > 0 {
				if sid, ok := ids[0].(string); ok {
					q.Set("sid", sid)
				}
			}
			pub, _ := v["publicKey"].(string)
			if pub == "" {
				if privateKey, ok := v["privateKey"].(string); ok {
					pub = realityPublicKey(privateKey)
				}
			}
			if pub != "" {
				q.Set("pbk", pub)
			}
			if fp, ok := v["fingerprint"].(string); ok && fp != "" {
				q.Set("fp", fp)
			} else {
				q.Set("fp", "chrome")
			}
			if spx, ok := v["spiderX"].(string); ok {
				q.Set("spx", spx)
			}
		}
	}
	if flow, ok := account["flow"].(string); ok && flow != "" {
		q.Set("flow", flow)
	} else if in.Protocol == "vless" && security == "reality" {
		q.Set("flow", "xtls-rprx-vision")
	}
	switch in.Protocol {
	case "vless":
		q.Set("encryption", "none")
		return fmt.Sprintf("vless://%s@%s:%d?%s#%s", credential, host, in.Port, q.Encode(), name), nil
	case "trojan":
		return fmt.Sprintf("trojan://%s@%s:%d?%s#%s", credential, host, in.Port, q.Encode(), name), nil
	case "vmess":
		tls := ""
		if security == "tls" || security == "reality" {
			tls = "tls"
		}
		path := ""
		hostHeader := ""
		if v, ok := ss["wsSettings"].(map[string]any); ok {
			path, _ = v["path"].(string)
			hostHeader, _ = v["host"].(string)
		}
		b, _ := json.Marshal(map[string]any{"v": "2", "ps": c.Email, "add": host, "port": in.Port, "id": credential, "aid": 0, "net": network, "type": "none", "host": hostHeader, "path": path, "tls": tls})
		return "vmess://" + base64.StdEncoding.EncodeToString(b), nil
	case "shadowsocks":
		method, _ := account["method"].(string)
		if method == "" {
			method = "chacha20-ietf-poly1305"
		}
		cred := base64.RawStdEncoding.EncodeToString([]byte(method + ":" + credential))
		return fmt.Sprintf("ss://%s@%s:%d#%s", cred, host, in.Port, name), nil
	case "socks", "http":
		return "", errors.New("subscription URI format for this protocol is not standardized")
	}
	return "", errors.New("unsupported protocol")
}

func realityPublicKey(privateKey string) string {
	b, err := base64.RawURLEncoding.DecodeString(privateKey)
	if err != nil {
		return ""
	}
	key, err := ecdh.X25519().NewPrivateKey(b)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
}
