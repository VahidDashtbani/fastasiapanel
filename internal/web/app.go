package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/skip2/go-qrcode"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fastasiapanel/internal/auth"
	"fastasiapanel/internal/config"
	"fastasiapanel/internal/store"
	"fastasiapanel/internal/xray"
)

var (
	errPortUsed   = errors.New("port is already used by another inbound")
	errEmailUsed  = errors.New("email already exists")
	errNotFound   = errors.New("not found")
	errBadInbound = errors.New("inbound does not exist")
)

type App struct {
	cfg     config.Config
	st      *store.Store
	au      *auth.Manager
	xr      *xray.Manager
	assets  fs.FS
	version string
	started time.Time
}

func New(cfg config.Config, st *store.Store, au *auth.Manager, xr *xray.Manager, assets fs.FS, version string) *App {
	return &App{cfg: cfg, st: st, au: au, xr: xr, assets: assets, version: version, started: time.Now()}
}

type clientView struct {
	ID          string `json:"id"`
	InboundID   string `json:"inbound_id"`
	Email       string `json:"email"`
	UUID        string `json:"uuid"`
	SubID       string `json:"sub_id"`
	TgID        string `json:"tg_id"`
	QuotaGroup  string `json:"quota_group"`
	HWID        string `json:"hwid"`
	Comment     string `json:"comment"`
	Enable      bool   `json:"enable"`
	Online      bool   `json:"online"`
	TotalBytes  int64  `json:"total_bytes"`
	ExpiryAt    int64  `json:"expiry_at"`
	FirstSeenAt int64  `json:"first_seen_at"`
	LimitIP     int    `json:"limit_ip"`
	Reset       int    `json:"reset"`
	Up          int64  `json:"up"`
	Down        int64  `json:"down"`
	Flow        string `json:"flow"`
	Link        string `json:"link"`
	SubURL      string `json:"sub_url"`
	LiveUp      int64  `json:"live_up"`
	LiveDown    int64  `json:"live_down"`
}

type inboundView struct {
	ID         string       `json:"id"`
	Tag        string       `json:"tag"`
	Remark     string       `json:"remark"`
	Protocol   string       `json:"protocol"`
	Listen     string       `json:"listen"`
	Port       int          `json:"port"`
	Enable     bool         `json:"enable"`
	TotalBytes int64        `json:"total_bytes"`
	ExpiryAt   int64        `json:"expiry_at"`
	Options    xray.Options `json:"options"`
	Up         int64        `json:"up"`
	Down       int64        `json:"down"`
	Clients    []clientView `json:"clients"`
}

type inboundReq struct {
	Remark     string       `json:"remark"`
	Protocol   string       `json:"protocol"`
	Listen     string       `json:"listen"`
	Port       int          `json:"port"`
	Enable     *bool        `json:"enable"`
	TotalBytes int64        `json:"total_bytes"`
	ExpiryAt   int64        `json:"expiry_at"`
	Options    xray.Options `json:"options"`
}

type clientReq struct {
	InboundID  string `json:"inbound_id"`
	Email      string `json:"email"`
	UUID       string `json:"uuid"`
	SubID      string `json:"sub_id"`
	TgID       string `json:"tg_id"`
	QuotaGroup string `json:"quota_group"`
	HWID       string `json:"hwid"`
	Comment    string `json:"comment"`
	Enable     *bool  `json:"enable"`
	TotalBytes int64  `json:"total_bytes"`
	ExpiryAt   int64  `json:"expiry_at"`
	LimitIP    int    `json:"limit_ip"`
	Reset      int    `json:"reset"`
	Flow       string `json:"flow"`
	BulkCount  int    `json:"bulk_count"`
}

type quotaGroupView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TotalBytes int64  `json:"total_bytes"`
	UsedBytes  int64  `json:"used_bytes"`
	Members    int    `json:"members"`
	Enable     bool   `json:"enable"`
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (a *App) host(r *http.Request) string {
	if custom := strings.TrimSpace(a.st.Setting("public_host")); custom != "" {
		return custom
	}
	if a.cfg.PublicHost != "" {
		return a.cfg.PublicHost
	}
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		return h
	}
	return r.Host
}

func (a *App) subBaseURL(r *http.Request) string {
	if custom := strings.TrimSpace(a.st.Setting("sub_base_url")); custom != "" {
		return strings.TrimRight(custom, "/")
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") || a.cfg.SecureCookies {
		scheme = "https"
	}
	host := strings.TrimSpace(a.host(r))
	if _, reqPort, err := net.SplitHostPort(strings.TrimSpace(r.Host)); err == nil && reqPort != "" {
		if _, _, err := net.SplitHostPort(host); err != nil {
			host = net.JoinHostPort(host, reqPort)
		}
	} else if _, _, err := net.SplitHostPort(host); err != nil && strings.Contains(a.cfg.Listen, ":") {
		parts := strings.Split(a.cfg.Listen, ":")
		listenPort := parts[len(parts)-1]
		if listenPort != "" && listenPort != "80" && listenPort != "443" {
			host = net.JoinHostPort(host, listenPort)
		}
	}
	base := strings.TrimRight(strings.TrimSpace(a.cfg.BasePath), "/")
	return fmt.Sprintf("%s://%s%s/sub", scheme, host, base)
}

func (a *App) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		scriptSrc := "'self'"
		if strings.Contains(r.URL.Path, "/sub/") {
			scriptSrc = "'self' 'unsafe-inline'"
		}
		h.Set("Content-Security-Policy", fmt.Sprintf("default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src %s; frame-ancestors 'none'; base-uri 'none'; form-action 'self'", scriptSrc))
		if a.cfg.SecureCookies {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func extractBearer(r *http.Request) string {
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	return ""
}

func (a *App) protect(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if tok := extractBearer(r); tok != "" {
			st := a.st.Snapshot()
			if store.ValidateAPIToken(&st, tok, scope, time.Now().UnixMilli()) {
				next(w, r)
				return
			}
			fail(w, http.StatusUnauthorized, "invalid or insufficient token scope")
			return
		}
		a.au.Require(next)(w, r)
	}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	b := a.cfg.BasePath
	req := a.au.Require

	mux.HandleFunc("GET "+b+"/{$}", a.index)
	mux.HandleFunc("GET "+b+"/dashboard", a.index)
	mux.HandleFunc("GET "+b+"/inbounds", a.index)
	mux.HandleFunc("GET "+b+"/clients", a.index)
	mux.HandleFunc("GET "+b+"/outbounds", a.index)
	mux.HandleFunc("GET "+b+"/routing", a.index)
	mux.HandleFunc("GET "+b+"/quotas", a.index)
	mux.HandleFunc("GET "+b+"/nodes", a.index)
	mux.HandleFunc("GET "+b+"/logs", a.index)
	mux.HandleFunc("GET "+b+"/settings", a.index)
	mux.HandleFunc("GET "+b+"/login", a.loginPage)
	mux.HandleFunc("GET "+b+"/static/app.js", a.asset("app.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET "+b+"/static/login.js", a.asset("login.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET "+b+"/static/style.css", a.asset("style.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET "+b+"/sub/{subId}", a.subscription)
	mux.HandleFunc("GET "+b+"/sub/qr", a.apiQR)
	mux.HandleFunc("GET "+b+"/api/qr", req(a.apiQR))

	mux.HandleFunc("POST "+b+"/api/login", a.login)
	mux.HandleFunc("POST "+b+"/api/logout", req(a.logout))
	mux.HandleFunc("POST "+b+"/api/password", req(a.password))
	mux.HandleFunc("GET "+b+"/api/status", a.protect("system:read", a.status))
	mux.HandleFunc("GET "+b+"/api/reality/keys", req(a.realityKeys))
	mux.HandleFunc("GET "+b+"/api/settings", req(a.getSettings))
	mux.HandleFunc("POST "+b+"/api/settings", req(a.saveSettings))

	mux.HandleFunc("GET "+b+"/api/inbounds", a.protect("inbounds:read", a.listInbounds))
	mux.HandleFunc("POST "+b+"/api/inbounds", a.protect("inbounds:write", a.createInbound))
	mux.HandleFunc("PUT "+b+"/api/inbounds/{id}", a.protect("inbounds:write", a.updateInbound))
	mux.HandleFunc("POST "+b+"/api/inbounds/{id}/toggle", a.protect("inbounds:write", a.toggleInbound))
	mux.HandleFunc("POST "+b+"/api/inbounds/{id}/reset", a.protect("inbounds:write", a.resetInbound))
	mux.HandleFunc("POST "+b+"/api/inbounds/reset-all", a.protect("inbounds:write", a.resetAllTraffic))
	mux.HandleFunc("DELETE "+b+"/api/inbounds/{id}", a.protect("inbounds:write", a.deleteInbound))

	mux.HandleFunc("POST "+b+"/api/clients", a.protect("clients:write", a.createClient))
	mux.HandleFunc("PUT "+b+"/api/clients/{id}", a.protect("clients:write", a.updateClient))
	mux.HandleFunc("POST "+b+"/api/clients/{id}/toggle", a.protect("clients:write", a.toggleClient))
	mux.HandleFunc("DELETE "+b+"/api/clients/{id}", a.protect("clients:write", a.deleteClient))
	mux.HandleFunc("POST "+b+"/api/clients/{id}/reset", a.protect("clients:write", a.resetClient))

	mux.HandleFunc("GET "+b+"/api/outbounds", req(a.listOutbounds))
	mux.HandleFunc("POST "+b+"/api/outbounds", req(a.createOutbound))
	mux.HandleFunc("POST "+b+"/api/outbounds/{id}/toggle", req(a.toggleOutbound))
	mux.HandleFunc("POST "+b+"/api/outbounds/{id}/test", req(a.testOutbound))
	mux.HandleFunc("POST "+b+"/api/outbounds/test-all", req(a.testAllOutbounds))
	mux.HandleFunc("DELETE "+b+"/api/outbounds/{id}", req(a.deleteOutbound))

	mux.HandleFunc("GET "+b+"/api/routing", req(a.listRouting))
	mux.HandleFunc("POST "+b+"/api/routing", req(a.createRouting))
	mux.HandleFunc("POST "+b+"/api/routing/preset", req(a.applyRoutingPreset))
	mux.HandleFunc("POST "+b+"/api/routing/{id}/toggle", req(a.toggleRouting))
	mux.HandleFunc("DELETE "+b+"/api/routing/{id}", req(a.deleteRouting))
	mux.HandleFunc("PUT "+b+"/api/routing/{id}", req(a.updateRouting))
	mux.HandleFunc("POST "+b+"/api/routing/{id}/move", req(a.moveRouting))
	mux.HandleFunc("POST "+b+"/api/routing/test", req(a.testRoutingRule))

	mux.HandleFunc("GET "+b+"/api/quota-groups", a.protect("inbounds:read", a.listQuotaGroups))
	mux.HandleFunc("POST "+b+"/api/quota-groups", a.protect("inbounds:write", a.createQuotaGroup))
	mux.HandleFunc("PUT "+b+"/api/quota-groups/{id}", a.protect("inbounds:write", a.updateQuotaGroup))
	mux.HandleFunc("DELETE "+b+"/api/quota-groups/{id}", a.protect("inbounds:write", a.deleteQuotaGroup))

	mux.HandleFunc("GET "+b+"/api/nodes", req(a.listNodes))
	mux.HandleFunc("POST "+b+"/api/nodes", req(a.createNode))
	mux.HandleFunc("POST "+b+"/api/nodes/{id}/ping", req(a.pingNode))
	mux.HandleFunc("DELETE "+b+"/api/nodes/{id}", req(a.deleteNode))

	mux.HandleFunc("GET "+b+"/api/tokens", req(a.listTokens))
	mux.HandleFunc("POST "+b+"/api/tokens", req(a.createToken))
	mux.HandleFunc("DELETE "+b+"/api/tokens/{id}", req(a.deleteToken))

	mux.HandleFunc("POST "+b+"/api/xray/restart", a.protect("system:manage", a.restartXray))
	mux.HandleFunc("GET "+b+"/api/xray/config", req(a.getXrayConfig))
	mux.HandleFunc("GET "+b+"/api/xray/logs", req(a.getXrayLogs))
	mux.HandleFunc("POST "+b+"/api/xray/geodata", req(a.updateGeoData))
	mux.HandleFunc("GET "+b+"/api/backup", req(a.backup))
	mux.HandleFunc("POST "+b+"/api/restore", req(a.restore))

	if b != "" {
		mux.HandleFunc("GET "+b, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, b+"/", http.StatusMovedPermanently)
		})
	}
	return a.secure(mux)
}

func isBrowserSubRequest(r *http.Request) bool {
	if r.URL.Query().Get("raw") == "1" || r.URL.Query().Get("format") != "" {
		return false
	}
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	clientHints := []string{
		"v2ray", "xray", "clash", "mihomo", "sing-box", "singbox",
		"shadowrocket", "surge", "loon", "quantumult", "nekobox",
		"nekoray", "hiddify", "streisand", "stash", "curl", "wget", "okhttp", "go-http-client",
	}
	for _, h := range clientHints {
		if strings.Contains(ua, h) {
			return false
		}
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, "text/html")
}

func fmtBytesFA(b int64) string {
	if b < 0 {
		b = 0
	}
	const kb = 1024.0
	const mb = kb * 1024.0
	const gb = mb * 1024.0
	f := float64(b)
	switch {
	case f >= gb:
		return fmt.Sprintf("%.2f GB", f/gb)
	case f >= mb:
		return fmt.Sprintf("%.2f MB", f/mb)
	case f >= kb:
		return fmt.Sprintf("%.1f KB", f/kb)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func escHTML(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	).Replace(s)
}

func (a *App) subscription(w http.ResponseWriter, r *http.Request) {
	subID := strings.TrimSpace(r.PathValue("subId"))
	if subID == "" {
		http.NotFound(w, r)
		return
	}
	st := a.st.Snapshot()
	res, ok := xray.BuildSubscription(st, subID, a.host(r))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Profile-Update-Interval", "12")
	if res.Title != "" {
		w.Header().Set("Profile-Title", "base64:"+encodeB64(res.Title))
	}
	userInfo := fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", res.Upload, res.Download, res.Total, res.ExpireSec)
	w.Header().Set("Subscription-Userinfo", userInfo)

	if isBrowserSubRequest(r) {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		subURL := fmt.Sprintf("%s://%s%s", scheme, r.Host, r.URL.Path)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		usedBytes := res.Upload + res.Download
		statusLabel := "Active"
		statusTone := ""
		progressPct := 0
		totalText := "Unlimited"
		remainText := "Unlimited"
		if res.Total > 0 {
			totalText = formatBytesEN(res.Total)
			rem := res.Total - usedBytes
			if rem < 0 {
				rem = 0
			}
			remainText = formatBytesEN(rem)
			pct := int((float64(usedBytes) / float64(res.Total)) * 100.0)
			if pct < 0 {
				pct = 0
			} else if pct > 100 {
				pct = 100
			}
			progressPct = pct
			if rem == 0 {
				statusLabel = "Quota Exceeded"
				statusTone = "danger"
			} else if pct >= 85 {
				statusTone = "warn"
			}
		}
		expireText := "Never"
		if res.ExpireSec > 0 {
			expireText = time.Unix(res.ExpireSec, 0).UTC().Format("2006-01-02 15:04 UTC")
		}
		emailTitle := strings.TrimSpace(res.Title)
		if emailTitle == "" {
			emailTitle = subID
		}
		pageData := subStatusPageData{
			ClientEmail:  emailTitle,
			SubID:        subID,
			SubURL:       subURL,
			StatusLabel:  statusLabel,
			StatusTone:   statusTone,
			UsedText:     formatBytesEN(usedBytes),
			TotalText:    totalText,
			RemainText:   remainText,
			ExpireText:   expireText,
			UploadText:   formatBytesEN(res.Upload),
			DownloadText: formatBytesEN(res.Download),
			ProgressPct:  progressPct,
			RawPayload:   res.Content,
		}
		renderSubStatusPage(w, r, pageData)
		return
	}

	if res.Title != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, url.PathEscape(res.Title)))
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(res.Content))
}

func (a *App) apiQR(w http.ResponseWriter, r *http.Request) {
	data := strings.TrimSpace(r.URL.Query().Get("data"))
	if data == "" {
		http.Error(w, "missing data", http.StatusBadRequest)
		return
	}
	if len(data) > 4096 {
		http.Error(w, "data too long", http.StatusBadRequest)
		return
	}
	// Render standard QR via qrcode package if imported; fallback cleanly if not present
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	if pngBytes, err := encodeStandardQRPNG(data); err == nil {
		_, _ = w.Write(pngBytes)
		return
	}
	http.Error(w, "qr encoder unavailable", http.StatusNotImplemented)
}

func encodeStandardQRPNG(data string) ([]byte, error) {
	return qrcode.Encode(data, qrcode.Medium, 256)
}

func encodeB64(s string) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := []byte(s)
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		var val uint32
		rem := len(b) - i
		val |= uint32(b[i]) << 16
		if rem > 1 {
			val |= uint32(b[i+1]) << 8
		}
		if rem > 2 {
			val |= uint32(b[i+2])
		}
		out.WriteByte(table[(val>>18)&0x3F])
		out.WriteByte(table[(val>>12)&0x3F])
		if rem > 1 {
			out.WriteByte(table[(val>>6)&0x3F])
		} else {
			out.WriteByte('=')
		}
		if rem > 2 {
			out.WriteByte(table[val&0x3F])
		} else {
			out.WriteByte('=')
		}
	}
	return out.String()
}

func (a *App) page(w http.ResponseWriter, name string) {
	data, err := fs.ReadFile(a.assets, name)
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	if !a.au.Authed(r) {
		http.Redirect(w, r, a.cfg.BasePath+"/login", http.StatusFound)
		return
	}
	a.page(w, "index.html")
}

func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	a.page(w, "login.html")
}

func (a *App) asset(name, ctype string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(a.assets, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(data)
	}
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		fail(w, http.StatusForbidden, "cross-origin request blocked")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	switch err := a.au.Login(w, r, req.Username, req.Password); {
	case errors.Is(err, auth.ErrLocked):
		fail(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, auth.ErrBadLogin):
		fail(w, http.StatusUnauthorized, err.Error())
	case err != nil:
		fail(w, http.StatusInternalServerError, "login failed")
	default:
		reply(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	a.au.Logout(w, r)
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) password(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := a.au.ChangePassword(req.Old, req.New); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	a.au.Logout(w, r)
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) status(w http.ResponseWriter, r *http.Request) {
	st := a.st.Snapshot()
	var up, down int64
	active := 0
	online := 0
	for _, c := range st.Clients {
		up += c.Up
		down += c.Down
		if c.Enable {
			active++
			if _, _, ok := a.xr.Live(c.Email); ok {
				online++
			}
		}
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	sys := collectSysStats()
	reply(w, http.StatusOK, map[string]any{
		"version":       a.version,
		"xray_running":  a.xr.Running(),
		"xray_version":  a.xr.Version(),
		"inbounds":      len(st.Inbounds),
		"outbounds":     len(st.Outbounds),
		"routing_rules": len(st.RoutingRules),
		"clients":       len(st.Clients),
		"active":        active,
		"online":        online,
		"up":            up,
		"down":          down,
		"uptime":        int64(time.Since(a.started).Seconds()),
		"panel_mem":     ms.Alloc,
		"db_type":       a.cfg.DBType,
		"sys":           sys,
	})
}

func (a *App) getSettings(w http.ResponseWriter, r *http.Request) {
	st := a.st.Snapshot()
	out := make(map[string]string, len(st.Settings))
	for k, v := range st.Settings {
		if k == "admin_hash" || k == "admin_user" {
			continue
		}
		out[k] = v
	}
	reply(w, http.StatusOK, out)
}

func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if !decode(w, r, &req) {
		return
	}
	err := a.st.Update(func(s *store.State) error {
		for k, v := range req {
			if k == "admin_hash" || k == "admin_user" {
				continue
			}
			s.Settings[k] = strings.TrimSpace(v)
		}
		return nil
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not save settings")
		return
	}
	a.xr.Schedule()
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) realityKeys(w http.ResponseWriter, r *http.Request) {
	priv, pub, err := xray.GenerateRealityKeys()
	if err != nil {
		fail(w, http.StatusInternalServerError, "key generation failed")
		return
	}
	reply(w, http.StatusOK, map[string]string{"private_key": priv, "public_key": pub, "short_id": xray.NewShortID()})
}

func (a *App) views(st store.State, host string, subBase string) []inboundView {
	out := make([]inboundView, 0, len(st.Inbounds))
	for _, in := range st.Inbounds {
		o, _ := xray.ParseOptions(in.Raw)
		v := inboundView{
			ID: in.ID, Tag: in.Tag, Remark: in.Remark, Protocol: in.Protocol,
			Listen: in.Listen, Port: in.Port, Enable: in.Enable,
			TotalBytes: in.TotalBytes, ExpiryAt: in.ExpiryAt,
			Up: in.Up, Down: in.Down, Options: o, Clients: []clientView{},
		}
		var clientUp, clientDown int64
		for _, c := range st.Clients {
			if c.InboundID != in.ID {
				continue
			}
			clientUp += c.Up
			clientDown += c.Down
			subURL := ""
			if c.SubID != "" {
				subURL = subBase + "/" + c.SubID
			}
			liveUp, liveDown, liveOk := a.xr.Live(c.Email)
			isOnline := c.Enable && liveOk
			v.Clients = append(v.Clients, clientView{
				ID: c.ID, InboundID: c.InboundID, Email: c.Email, UUID: c.UUID,
				SubID: c.SubID, TgID: c.TgID, QuotaGroup: c.QuotaGroup, HWID: c.HWID,
				Comment: c.Comment, Enable: c.Enable, Online: isOnline,
				TotalBytes: c.TotalBytes, ExpiryAt: c.ExpiryAt, FirstSeenAt: c.FirstSeenAt,
				LimitIP: c.LimitIP, Reset: c.Reset, Up: c.Up, Down: c.Down,
				LiveUp: liveUp, LiveDown: liveDown,
				Flow: xray.ClientFlow(c), Link: xray.BuildLink(in, c, host), SubURL: subURL,
			})
		}
		if v.Up == 0 && clientUp > 0 {
			v.Up = clientUp
		}
		if v.Down == 0 && clientDown > 0 {
			v.Down = clientDown
		}
		out = append(out, v)
	}
	return out
}

func (a *App) listInbounds(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, a.views(a.st.Snapshot(), a.host(r), a.subBaseURL(r)))
}

func inboundFromReq(req inboundReq) (store.Inbound, error) {
	o := req.Options
	if o.Network == "" {
		o.Network = "tcp"
	}
	if o.Security == "" {
		o.Security = "none"
	}
	xray.Normalize(req.Protocol, &o)
	if o.Security == "reality" {
		if o.Reality.PrivateKey == "" {
			priv, pub, err := xray.GenerateRealityKeys()
			if err != nil {
				return store.Inbound{}, err
			}
			o.Reality.PrivateKey, o.Reality.PublicKey = priv, pub
		} else if o.Reality.PublicKey == "" {
			pub, err := xray.PublicFromPrivate(o.Reality.PrivateKey)
			if err != nil {
				return store.Inbound{}, errors.New("reality private key is invalid")
			}
			o.Reality.PublicKey = pub
		}
		if o.Reality.ShortID == "" {
			o.Reality.ShortID = xray.NewShortID()
		}
	}
	raw, err := json.Marshal(o)
	if err != nil {
		return store.Inbound{}, err
	}
	in := store.Inbound{
		Remark: strings.TrimSpace(req.Remark), Protocol: req.Protocol, Listen: strings.TrimSpace(req.Listen),
		Port: req.Port, Enable: true, TotalBytes: req.TotalBytes, ExpiryAt: req.ExpiryAt, Raw: raw,
	}
	if req.Enable != nil {
		in.Enable = *req.Enable
	}
	if err := xray.ValidateInbound(in, o); err != nil {
		return in, err
	}
	return in, nil
}

func (a *App) createInbound(w http.ResponseWriter, r *http.Request) {
	var req inboundReq
	if !decode(w, r, &req) {
		return
	}
	in, err := inboundFromReq(req)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := store.NewID()
	if err != nil {
		fail(w, http.StatusInternalServerError, "id generation failed")
		return
	}
	now := time.Now().UTC()
	in.ID, in.Tag, in.CreatedAt, in.UpdatedAt = id, "in-"+id[:8], now, now
	err = a.st.Update(func(s *store.State) error {
		for _, o := range s.Inbounds {
			if o.Port == in.Port {
				return errPortUsed
			}
		}
		s.Inbounds = append(s.Inbounds, in)
		return nil
	})
	if errors.Is(err, errPortUsed) {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not save")
		return
	}
	a.xr.Schedule()
	reply(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) updateInbound(w http.ResponseWriter, r *http.Request) {
	var req inboundReq
	if !decode(w, r, &req) {
		return
	}
	next, err := inboundFromReq(req)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	id := r.PathValue("id")
	err = a.st.Update(func(s *store.State) error {
		i, cur := store.FindInbound(s, id)
		if cur == nil {
			return errNotFound
		}
		for _, o := range s.Inbounds {
			if o.ID != id && o.Port == next.Port {
				return errPortUsed
			}
		}
		next.ID, next.Tag, next.Up, next.Down, next.CreatedAt, next.UpdatedAt = cur.ID, cur.Tag, cur.Up, cur.Down, cur.CreatedAt, time.Now().UTC()
		s.Inbounds[i] = next
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) toggleInbound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var state bool
	err := a.st.Update(func(s *store.State) error {
		_, in := store.FindInbound(s, id)
		if in == nil {
			return errNotFound
		}
		in.Enable = !in.Enable
		in.UpdatedAt = time.Now().UTC()
		state = in.Enable
		return nil
	})
	a.finish(w, err, map[string]bool{"enable": state})
}

func (a *App) resetInbound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		_, in := store.FindInbound(s, id)
		if in == nil {
			return errNotFound
		}
		in.Up, in.Down = 0, 0
		for i := range s.Clients {
			if s.Clients[i].InboundID == id {
				s.Clients[i].Up = 0
				s.Clients[i].Down = 0
			}
		}
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) resetAllTraffic(w http.ResponseWriter, r *http.Request) {
	err := a.st.Update(func(s *store.State) error {
		for i := range s.Inbounds {
			s.Inbounds[i].Up, s.Inbounds[i].Down = 0, 0
		}
		for i := range s.Clients {
			s.Clients[i].Up, s.Clients[i].Down = 0, 0
		}
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) deleteInbound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		i, in := store.FindInbound(s, id)
		if in == nil {
			return errNotFound
		}
		s.Inbounds = append(s.Inbounds[:i], s.Inbounds[i+1:]...)
		kept := s.Clients[:0]
		for _, c := range s.Clients {
			if c.InboundID != id {
				kept = append(kept, c)
			}
		}
		s.Clients = kept
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) finish(w http.ResponseWriter, err error, ok any) {
	switch {
	case errors.Is(err, errNotFound):
		fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errPortUsed), errors.Is(err, errEmailUsed):
		fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, errBadInbound):
		fail(w, http.StatusBadRequest, err.Error())
	case err != nil:
		fail(w, http.StatusBadRequest, err.Error())
	default:
		a.xr.Schedule()
		reply(w, http.StatusOK, ok)
	}
}

func validateClient(req *clientReq) error {
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || len(req.Email) > 64 {
		return errors.New("email must be 1 to 64 characters")
	}
	if strings.ContainsAny(req.Email, " \t\r\n>#/?") {
		return errors.New("email must not contain spaces or the characters > # / ?")
	}
	if req.TotalBytes < 0 {
		return errors.New("total bytes must not be negative")
	}
	if req.LimitIP < 0 || req.Reset < 0 {
		return errors.New("limit_ip and reset must not be negative")
	}
	return nil
}

func emailTaken(s *store.State, email, except string) bool {
	for _, c := range s.Clients {
		if c.Email == email && c.ID != except {
			return true
		}
	}
	return false
}

func (a *App) createClient(w http.ResponseWriter, r *http.Request) {
	var req clientReq
	if !decode(w, r, &req) {
		return
	}
	if err := validateClient(&req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	count := req.BulkCount
	if count <= 0 {
		count = 1
	}
	if count > 200 {
		fail(w, http.StatusBadRequest, "bulk_count cannot exceed 200")
		return
	}

	var firstID string
	err := a.st.Update(func(s *store.State) error {
		_, in := store.FindInbound(s, req.InboundID)
		if in == nil {
			return errBadInbound
		}
		o, _ := xray.ParseOptions(in.Raw)
		if err := xray.ValidateFlow(*in, o, req.Flow); err != nil {
			return err
		}
		enable := true
		if req.Enable != nil {
			enable = *req.Enable
		}
		now := time.Now().UTC()
		for n := 0; n < count; n++ {
			email := req.Email
			if count > 1 {
				email = req.Email + "-" + strconv.Itoa(n+1)
			}
			if emailTaken(s, email, "") {
				return errEmailUsed
			}
			id, err := store.NewID()
			if err != nil {
				return err
			}
			if firstID == "" {
				firstID = id
			}
			subID := strings.TrimSpace(req.SubID)
			if subID == "" || count > 1 {
				subID, _ = store.NewSubID()
			}
			uuid := strings.TrimSpace(req.UUID)
			if uuid == "" || count > 1 {
				uuid = xray.NewCredential(in.Protocol)
			}
			s.Clients = append(s.Clients, store.Client{
				ID: id, InboundID: in.ID, Email: email,
				Config: xray.EncodeClientConfig(req.Flow), Flow: req.Flow,
				UUID: uuid, SubID: subID, TgID: strings.TrimSpace(req.TgID),
				QuotaGroup: strings.TrimSpace(req.QuotaGroup), HWID: strings.TrimSpace(req.HWID),
				Comment: strings.TrimSpace(req.Comment), Enable: enable,
				TotalBytes: req.TotalBytes, ExpiryAt: req.ExpiryAt,
				LimitIP: req.LimitIP, Reset: req.Reset, CreatedAt: now,
			})
		}
		store.EnforceQuotaGroups(s)
		return nil
	})
	if err != nil {
		a.finish(w, err, nil)
		return
	}
	a.xr.Schedule()
	reply(w, http.StatusCreated, map[string]string{"id": firstID})
}

func (a *App) updateClient(w http.ResponseWriter, r *http.Request) {
	var req clientReq
	if !decode(w, r, &req) {
		return
	}
	if err := validateClient(&req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		_, c := store.FindClient(s, id)
		if c == nil {
			return errNotFound
		}
		_, in := store.FindInbound(s, c.InboundID)
		if in == nil {
			return errBadInbound
		}
		o, _ := xray.ParseOptions(in.Raw)
		if err := xray.ValidateFlow(*in, o, req.Flow); err != nil {
			return err
		}
		if emailTaken(s, req.Email, id) {
			return errEmailUsed
		}
		c.Email = req.Email
		c.TotalBytes = req.TotalBytes
		c.ExpiryAt = req.ExpiryAt
		c.LimitIP = req.LimitIP
		c.Reset = req.Reset
		c.TgID = strings.TrimSpace(req.TgID)
		c.QuotaGroup = strings.TrimSpace(req.QuotaGroup)
		c.HWID = strings.TrimSpace(req.HWID)
		c.Comment = strings.TrimSpace(req.Comment)
		c.Flow = req.Flow
		c.Config = xray.EncodeClientConfig(req.Flow)
		if u := strings.TrimSpace(req.UUID); u != "" {
			c.UUID = u
		}
		if sub := strings.TrimSpace(req.SubID); sub != "" {
			c.SubID = sub
		} else if c.SubID == "" {
			c.SubID, _ = store.NewSubID()
		}
		if req.Enable != nil {
			c.Enable = *req.Enable
		}
		store.EnforceQuotaGroups(s)
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) toggleClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var state bool
	err := a.st.Update(func(s *store.State) error {
		_, c := store.FindClient(s, id)
		if c == nil {
			return errNotFound
		}
		c.Enable = !c.Enable
		state = c.Enable
		return nil
	})
	a.finish(w, err, map[string]bool{"enable": state})
}

func (a *App) deleteClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		i, c := store.FindClient(s, id)
		if c == nil {
			return errNotFound
		}
		s.Clients = append(s.Clients[:i], s.Clients[i+1:]...)
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) resetClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		_, c := store.FindClient(s, id)
		if c == nil {
			return errNotFound
		}
		c.Up, c.Down, c.Enable = 0, 0, true
		c.LastResetAt = time.Now().UnixMilli()
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) listOutbounds(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, a.st.Snapshot().Outbounds)
}

func (a *App) createOutbound(w http.ResponseWriter, r *http.Request) {
	var req store.Outbound
	if !decode(w, r, &req) {
		return
	}
	req.Tag = strings.TrimSpace(req.Tag)
	req.Remark = strings.TrimSpace(req.Remark)
	req.Protocol = strings.TrimSpace(req.Protocol)
	if req.Tag == "" || req.Protocol == "" {
		fail(w, http.StatusBadRequest, "tag and protocol are required")
		return
	}
	if req.Tag == "direct" || req.Tag == "blocked" || req.Tag == "api" {
		fail(w, http.StatusBadRequest, "reserved outbound tag")
		return
	}
	id, err := store.NewID()
	if err != nil {
		fail(w, http.StatusInternalServerError, "id generation failed")
		return
	}
	req.ID = id
	req.Enable = true
	err = a.st.Update(func(s *store.State) error {
		for _, o := range s.Outbounds {
			if o.Tag == req.Tag {
				return errors.New("outbound tag already exists")
			}
		}
		s.Outbounds = append(s.Outbounds, req)
		return nil
	})
	a.finish(w, err, map[string]string{"id": id})
}

func (a *App) updateOutbound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req store.Outbound
	if !decode(w, r, &req) {
		return
	}
	req.Tag = strings.TrimSpace(req.Tag)
	req.Remark = strings.TrimSpace(req.Remark)
	req.Protocol = strings.TrimSpace(req.Protocol)
	if req.Tag == "" || req.Protocol == "" {
		fail(w, http.StatusBadRequest, "tag and protocol are required")
		return
	}
	if req.Tag == "direct" || req.Tag == "blocked" || req.Tag == "api" {
		fail(w, http.StatusBadRequest, "reserved outbound tag")
		return
	}
	err := a.st.Update(func(s *store.State) error {
		for _, o := range s.Outbounds {
			if o.ID != id && o.Tag == req.Tag {
				return errors.New("outbound tag already exists")
			}
		}
		for i := range s.Outbounds {
			if s.Outbounds[i].ID == id {
				req.ID = id
				req.Enable = s.Outbounds[i].Enable
				s.Outbounds[i] = req
				return nil
			}
		}
		return errNotFound
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) toggleOutbound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		for i := range s.Outbounds {
			if s.Outbounds[i].ID == id {
				s.Outbounds[i].Enable = !s.Outbounds[i].Enable
				return nil
			}
		}
		return errNotFound
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

type outboundTestResult struct {
	ID        string `json:"id"`
	Tag       string `json:"tag"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func probeOutbound(ob store.Outbound) outboundTestResult {
	res := outboundTestResult{
		ID:  ob.ID,
		Tag: ob.Tag,
	}
	proto := strings.ToLower(strings.TrimSpace(ob.Protocol))
	if proto == "freedom" || proto == "blackhole" {
		res.OK = true
		res.LatencyMS = 0
		res.Status = "Built-in (" + proto + ")"
		return res
	}
	addr := strings.TrimSpace(ob.Address)
	if addr == "" || ob.Port <= 0 {
		res.OK = false
		res.Status = "Invalid Endpoint"
		res.Error = "address or port is empty"
		return res
	}
	target := net.JoinHostPort(addr, fmt.Sprintf("%d", ob.Port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, 4*time.Second)
	if err != nil {
		res.OK = false
		res.LatencyMS = time.Since(start).Milliseconds()
		res.Status = "Unreachable"
		res.Error = err.Error()
		return res
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))

	switch proto {
	case "socks", "socks5":
		var greet []byte
		if ob.Username != "" || ob.Password != "" {
			greet = []byte{0x05, 0x02, 0x00, 0x02}
		} else {
			greet = []byte{0x05, 0x01, 0x00}
		}
		if _, err := conn.Write(greet); err != nil {
			res.OK = false
			res.LatencyMS = time.Since(start).Milliseconds()
			res.Status = "Handshake Failed"
			res.Error = err.Error()
			return res
		}
		resp := make([]byte, 2)
		if _, err := conn.Read(resp); err != nil || resp[0] != 0x05 || resp[1] == 0xff {
			res.OK = false
			res.LatencyMS = time.Since(start).Milliseconds()
			res.Status = "SOCKS5 Rejected"
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Error = "no acceptable auth method"
			}
			return res
		}
		if resp[1] == 0x02 {
			u := []byte(ob.Username)
			p := []byte(ob.Password)
			if len(u) > 255 || len(p) > 255 {
				res.OK = false
				res.LatencyMS = time.Since(start).Milliseconds()
				res.Status = "Auth Error"
				res.Error = "credentials too long"
				return res
			}
			auth := make([]byte, 0, 3+len(u)+len(p))
			auth = append(auth, 0x01, byte(len(u)))
			auth = append(auth, u...)
			auth = append(auth, byte(len(p)))
			auth = append(auth, p...)
			if _, err := conn.Write(auth); err != nil {
				res.OK = false
				res.LatencyMS = time.Since(start).Milliseconds()
				res.Status = "Auth Write Failed"
				res.Error = err.Error()
				return res
			}
			aresp := make([]byte, 2)
			if _, err := conn.Read(aresp); err != nil || aresp[1] != 0x00 {
				res.OK = false
				res.LatencyMS = time.Since(start).Milliseconds()
				res.Status = "Auth Failed"
				if err != nil {
					res.Error = err.Error()
				} else {
					res.Error = "invalid username/password"
				}
				return res
			}
		}
	case "http":
		reqStr := fmt.Sprintf("CONNECT 1.1.1.1:443 HTTP/1.1\r\nHost: 1.1.1.1:443\r\n\r\n")
		if _, err := conn.Write([]byte(reqStr)); err != nil {
			res.OK = false
			res.LatencyMS = time.Since(start).Milliseconds()
			res.Status = "HTTP Write Failed"
			res.Error = err.Error()
			return res
		}
		buf := make([]byte, 64)
		n, err := conn.Read(buf)
		if err != nil || n == 0 || (!strings.Contains(string(buf[:n]), "200") && !strings.Contains(string(buf[:n]), "407")) {
			res.OK = false
			res.LatencyMS = time.Since(start).Milliseconds()
			res.Status = "HTTP Proxy Error"
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Error = strings.TrimSpace(string(buf[:n]))
			}
			return res
		}
	}

	res.OK = true
	res.LatencyMS = time.Since(start).Milliseconds()
	if res.LatencyMS < 1 {
		res.LatencyMS = 1
	}
	res.Status = "Healthy"
	return res
}

func (a *App) testOutbound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snap := a.st.Snapshot()
	for _, ob := range snap.Outbounds {
		if ob.ID == id || ob.Tag == id {
			reply(w, http.StatusOK, probeOutbound(ob))
			return
		}
	}
	fail(w, http.StatusNotFound, "outbound not found")
}

func (a *App) testAllOutbounds(w http.ResponseWriter, r *http.Request) {
	snap := a.st.Snapshot()
	results := make([]outboundTestResult, len(snap.Outbounds))
	for i, ob := range snap.Outbounds {
		results[i] = probeOutbound(ob)
	}
	reply(w, http.StatusOK, results)
}

func (a *App) deleteOutbound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		found := false
		kept := s.Outbounds[:0]
		for _, o := range s.Outbounds {
			if o.ID == id {
				found = true
				continue
			}
			kept = append(kept, o)
		}
		if !found {
			return errNotFound
		}
		s.Outbounds = kept
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) listRouting(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, a.st.Snapshot().RoutingRules)
}

func (a *App) createRouting(w http.ResponseWriter, r *http.Request) {
	var req store.RoutingRule
	if !decode(w, r, &req) {
		return
	}
	req.Remark = strings.TrimSpace(req.Remark)
	req.OutboundTag = strings.TrimSpace(req.OutboundTag)
	req.Port = strings.TrimSpace(req.Port)

	if req.OutboundTag == "" {
		fail(w, http.StatusBadRequest, "outbound_tag is required")
		return
	}
	if req.Remark == "" {
		req.Remark = req.OutboundTag + " rule"
	}
	if req.Priority == 0 {
		req.Priority = 50
	}

	cleanSlice := func(items []string) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			v := strings.TrimSpace(item)
			if v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	req.Domains = cleanSlice(req.Domains)
	req.IPs = cleanSlice(req.IPs)
	req.InboundTags = cleanSlice(req.InboundTags)
	req.Protocol = cleanSlice(req.Protocol)

	id, err := store.NewID()
	if err != nil {
		fail(w, http.StatusInternalServerError, "id generation failed")
		return
	}
	req.ID = id
	req.Enable = true

	err = a.st.Update(func(s *store.State) error {
		s.RoutingRules = append(s.RoutingRules, req)
		return nil
	})
	a.finish(w, err, map[string]string{"id": id})
}

func (a *App) applyRoutingPreset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Preset      string `json:"preset"`
		OutboundTag string `json:"outbound_tag"`
	}
	if !decode(w, r, &req) {
		return
	}
	id, _ := store.NewID()
	var rule store.RoutingRule
	switch req.Preset {
	case "block-ads":
		rule = store.RoutingRule{
			ID: id, Remark: "مسدودسازی تبلیغات و ردیاب‌ها", OutboundTag: "blocked",
			Domains: []string{"geosite:category-ads-all"}, Priority: 10, Enable: true,
		}
	case "block-porn":
		rule = store.RoutingRule{
			ID: id, Remark: "مسدودسازی محتوای غیراخلاقی", OutboundTag: "blocked",
			Domains: []string{"geosite:category-porn"}, Priority: 11, Enable: true,
		}
	case "ir-direct":
		rule = store.RoutingRule{
			ID: id, Remark: "عبور مستقیم ترافیک ایران", OutboundTag: "direct",
			Domains: []string{"geosite:ir", "regexp:.*\\.ir$"}, IPs: []string{"geoip:ir"}, Priority: 20, Enable: true,
		}
	case "ai-outbound":
		tag := strings.TrimSpace(req.OutboundTag)
		if tag == "" {
			tag = "direct"
		}
		rule = store.RoutingRule{
			ID: id, Remark: "مسیریابی سرویس‌های هوش مصنوعی (OpenAI / Claude)", OutboundTag: tag,
			Domains: []string{"geosite:openai", "geosite:anthropic", "domain:chatgpt.com"}, Priority: 15, Enable: true,
		}
	default:
		fail(w, http.StatusBadRequest, "unknown preset")
		return
	}
	err := a.st.Update(func(s *store.State) error {
		s.RoutingRules = append(s.RoutingRules, rule)
		return nil
	})
	a.finish(w, err, map[string]string{"id": id})
}

func (a *App) toggleRouting(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		for i := range s.RoutingRules {
			if s.RoutingRules[i].ID == id {
				s.RoutingRules[i].Enable = !s.RoutingRules[i].Enable
				return nil
			}
		}
		return errNotFound
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) deleteRouting(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		found := false
		kept := s.RoutingRules[:0]
		for _, rl := range s.RoutingRules {
			if rl.ID == id {
				found = true
				continue
			}
			kept = append(kept, rl)
		}
		if !found {
			return errNotFound
		}
		s.RoutingRules = kept
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func cleanStringSlice(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		v := strings.TrimSpace(item)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (a *App) updateRouting(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req store.RoutingRule
	if !decode(w, r, &req) {
		return
	}
	req.Remark = strings.TrimSpace(req.Remark)
	req.OutboundTag = strings.TrimSpace(req.OutboundTag)
	req.Port = strings.TrimSpace(req.Port)
	req.Domains = cleanStringSlice(req.Domains)
	req.IPs = cleanStringSlice(req.IPs)
	req.InboundTags = cleanStringSlice(req.InboundTags)
	req.Protocol = cleanStringSlice(req.Protocol)

	if req.OutboundTag == "" {
		fail(w, http.StatusBadRequest, "outbound_tag is required")
		return
	}
	if req.Remark == "" {
		req.Remark = req.OutboundTag + " rule"
	}

	err := a.st.Update(func(s *store.State) error {
		for i := range s.RoutingRules {
			if s.RoutingRules[i].ID == id {
				req.ID = id
				req.Enable = s.RoutingRules[i].Enable
				s.RoutingRules[i] = req
				return nil
			}
		}
		return errNotFound
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) moveRouting(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Direction string `json:"direction"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := a.st.Update(func(s *store.State) error {
		idx := -1
		for i := range s.RoutingRules {
			if s.RoutingRules[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return errNotFound
		}
		delta := -1
		if req.Direction == "down" {
			delta = 1
		}
		target := idx + delta
		if target < 0 || target >= len(s.RoutingRules) {
			return nil
		}
		s.RoutingRules[idx], s.RoutingRules[target] = s.RoutingRules[target], s.RoutingRules[idx]
		if s.RoutingRules[idx].Priority == s.RoutingRules[target].Priority {
			s.RoutingRules[idx].Priority = (target + 1) * 10
			s.RoutingRules[target].Priority = (idx + 1) * 10
		}
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) testRoutingRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target     string `json:"target"`
		Port       string `json:"port"`
		InboundTag string `json:"inbound_tag"`
	}
	if !decode(w, r, &req) {
		return
	}
	rawTarget := strings.TrimSpace(req.Target)
	port := strings.TrimSpace(req.Port)
	inbound := strings.TrimSpace(req.InboundTag)

	if rawTarget == "" && port == "" && inbound == "" {
		fail(w, http.StatusBadRequest, "target, port, or inbound_tag is required")
		return
	}

	host := rawTarget
	if host != "" && port == "" && strings.Contains(rawTarget, ":") && !strings.Contains(rawTarget, "::") {
		parts := strings.SplitN(rawTarget, ":", 2)
		host = strings.TrimSpace(parts[0])
		if port == "" {
			port = strings.TrimSpace(parts[1])
		}
	}

	snap := a.st.Snapshot()
	rules := make([]store.RoutingRule, len(snap.RoutingRules))
	copy(rules, snap.RoutingRules)

	for _, rl := range rules {
		if !rl.Enable {
			continue
		}
		if len(rl.InboundTags) > 0 && inbound != "" && !containsFold(rl.InboundTags, inbound) {
			continue
		}
		if len(rl.InboundTags) > 0 && inbound == "" {
			continue
		}
		if rl.Port != "" && port != "" && !portMatches(rl.Port, port) {
			continue
		}
		if len(rl.Domains) > 0 && host != "" && matchAnyPattern(rl.Domains, host) {
			reply(w, http.StatusOK, map[string]any{
				"matched":      true,
				"rule_id":      rl.ID,
				"rule_name":    rl.Remark,
				"outbound_tag": rl.OutboundTag,
				"reason":       "Matched domain rule",
			})
			return
		}
		if len(rl.IPs) > 0 && host != "" && matchAnyPattern(rl.IPs, host) {
			reply(w, http.StatusOK, map[string]any{
				"matched":      true,
				"rule_id":      rl.ID,
				"rule_name":    rl.Remark,
				"outbound_tag": rl.OutboundTag,
				"reason":       "Matched IP/CIDR rule",
			})
			return
		}
		if len(rl.Domains) == 0 && len(rl.IPs) == 0 && (rl.Port != "" || len(rl.InboundTags) > 0) {
			reply(w, http.StatusOK, map[string]any{
				"matched":      true,
				"rule_id":      rl.ID,
				"rule_name":    rl.Remark,
				"outbound_tag": rl.OutboundTag,
				"reason":       "Matched port/inbound rule",
			})
			return
		}
	}

	reply(w, http.StatusOK, map[string]any{
		"matched":      false,
		"outbound_tag": "direct",
		"reason":       "No custom rule matched; fallback to default/direct outbound",
	})
}

func containsFold(list []string, target string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), target) {
			return true
		}
	}
	return false
}

func portMatches(rulePorts, targetPort string) bool {
	for _, p := range strings.Split(rulePorts, ",") {
		p = strings.TrimSpace(p)
		if p == targetPort {
			return true
		}
		if strings.Contains(p, "-") {
			bounds := strings.SplitN(p, "-", 2)
			a, errA := strconv.Atoi(strings.TrimSpace(bounds[0]))
			b, errB := strconv.Atoi(strings.TrimSpace(bounds[1]))
			t, errT := strconv.Atoi(strings.TrimSpace(targetPort))
			if errA == nil && errB == nil && errT == nil && t >= a && t <= b {
				return true
			}
		}
	}
	return false
}

func matchAnyPattern(patterns []string, val string) bool {
	v := strings.ToLower(strings.TrimSpace(val))
	for _, raw := range patterns {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == "" {
			continue
		}
		switch {
		case strings.HasPrefix(p, "domain:"):
			d := strings.TrimPrefix(p, "domain:")
			if v == d || strings.HasSuffix(v, "."+d) {
				return true
			}
		case strings.HasPrefix(p, "full:"):
			if v == strings.TrimPrefix(p, "full:") {
				return true
			}
		case strings.HasPrefix(p, "geosite:"):
			site := strings.TrimPrefix(p, "geosite:")
			if strings.Contains(v, site) {
				return true
			}
		case strings.HasPrefix(p, "geoip:"):
			ipTag := strings.TrimPrefix(p, "geoip:")
			if ipTag == "ir" && strings.HasSuffix(v, ".ir") {
				return true
			}
		default:
			if strings.Contains(p, "/") {
				if _, ipNet, err := net.ParseCIDR(p); err == nil {
					if parsedIP := net.ParseIP(v); parsedIP != nil && ipNet.Contains(parsedIP) {
						return true
					}
				}
			} else if v == p || strings.HasSuffix(v, "."+p) {
				return true
			}
		}
	}
	return false
}

func (a *App) listQuotaGroups(w http.ResponseWriter, r *http.Request) {
	st := a.st.Snapshot()
	out := make([]quotaGroupView, 0, len(st.QuotaGroups))
	for _, g := range st.QuotaGroups {
		members := 0
		for _, c := range st.Clients {
			if c.QuotaGroup == g.ID {
				members++
			}
		}
		out = append(out, quotaGroupView{
			ID: g.ID, Name: g.Name, TotalBytes: g.TotalBytes,
			UsedBytes: store.GroupUsedBytes(&st, g.ID), Members: members, Enable: g.Enable,
		})
	}
	reply(w, http.StatusOK, out)
}

func (a *App) createQuotaGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		TotalBytes int64  `json:"total_bytes"`
		Enable     *bool  `json:"enable"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	id, err := store.NewID()
	if err != nil {
		fail(w, http.StatusInternalServerError, "id generation failed")
		return
	}
	en := true
	if req.Enable != nil {
		en = *req.Enable
	}
	err = a.st.Update(func(s *store.State) error {
		s.QuotaGroups = append(s.QuotaGroups, store.QuotaGroup{
			ID: id, Name: req.Name, TotalBytes: req.TotalBytes, Enable: en,
		})
		return nil
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not save")
		return
	}
	reply(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) updateQuotaGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Name       string `json:"name"`
		TotalBytes int64  `json:"total_bytes"`
		Enable     *bool  `json:"enable"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := a.st.Update(func(s *store.State) error {
		for i := range s.QuotaGroups {
			if s.QuotaGroups[i].ID == id {
				if n := strings.TrimSpace(req.Name); n != "" {
					s.QuotaGroups[i].Name = n
				}
				s.QuotaGroups[i].TotalBytes = req.TotalBytes
				if req.Enable != nil {
					s.QuotaGroups[i].Enable = *req.Enable
				}
				store.EnforceQuotaGroups(s)
				return nil
			}
		}
		return errNotFound
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) deleteQuotaGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		found := false
		kept := s.QuotaGroups[:0]
		for _, g := range s.QuotaGroups {
			if g.ID == id {
				found = true
				continue
			}
			kept = append(kept, g)
		}
		if !found {
			return errNotFound
		}
		s.QuotaGroups = kept
		for i := range s.Clients {
			if s.Clients[i].QuotaGroup == id {
				s.Clients[i].QuotaGroup = ""
			}
		}
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) listNodes(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, a.st.Snapshot().Nodes)
}

func (a *App) createNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Address string `json:"address"`
		Token   string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Address = strings.TrimRight(strings.TrimSpace(req.Address), "/")
	req.Token = strings.TrimSpace(req.Token)
	if req.Name == "" || req.Address == "" || req.Token == "" {
		fail(w, http.StatusBadRequest, "name, address and token are required")
		return
	}
	id, err := store.NewID()
	if err != nil {
		fail(w, http.StatusInternalServerError, "id generation failed")
		return
	}
	err = a.st.Update(func(s *store.State) error {
		s.Nodes = append(s.Nodes, store.Node{
			ID: id, Name: req.Name, Address: req.Address, Token: req.Token, Status: "pending",
		})
		return nil
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not save")
		return
	}
	reply(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) pingNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st := a.st.Snapshot()
	var target *store.Node
	for i := range st.Nodes {
		if st.Nodes[i].ID == id {
			target = &st.Nodes[i]
			break
		}
	}
	if target == nil {
		fail(w, http.StatusNotFound, errNotFound.Error())
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", target.Address+"/api/status", nil)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid node url")
		return
	}
	req.Header.Set("Authorization", "Bearer "+target.Token)
	resp, err := client.Do(req)
	status := "offline"
	var remote map[string]any
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			status = "online"
			_ = json.NewDecoder(resp.Body).Decode(&remote)
		}
	}
	_ = a.st.Update(func(s *store.State) error {
		for i := range s.Nodes {
			if s.Nodes[i].ID == id {
				s.Nodes[i].Status = status
				break
			}
		}
		return nil
	})
	reply(w, http.StatusOK, map[string]any{"status": status, "remote": remote})
}

func (a *App) deleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		found := false
		kept := s.Nodes[:0]
		for _, n := range s.Nodes {
			if n.ID == id {
				found = true
				continue
			}
			kept = append(kept, n)
		}
		if !found {
			return errNotFound
		}
		s.Nodes = kept
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) listTokens(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, a.st.Snapshot().APITokens)
}

func (a *App) createToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		Scope     string `json:"scope"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Scope = strings.TrimSpace(req.Scope)
	if req.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Scope == "" {
		req.Scope = "*"
	}
	id, err := store.NewID()
	if err != nil {
		fail(w, http.StatusInternalServerError, "id generation failed")
		return
	}
	tok, err := store.NewToken()
	if err != nil {
		fail(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	item := store.APIToken{
		ID: id, Name: req.Name, Token: tok, Scope: req.Scope, ExpiresAt: req.ExpiresAt,
	}
	err = a.st.Update(func(s *store.State) error {
		s.APITokens = append(s.APITokens, item)
		return nil
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not save")
		return
	}
	reply(w, http.StatusCreated, item)
}

func (a *App) deleteToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.st.Update(func(s *store.State) error {
		found := false
		kept := s.APITokens[:0]
		for _, t := range s.APITokens {
			if t.ID == id {
				found = true
				continue
			}
			kept = append(kept, t)
		}
		if !found {
			return errNotFound
		}
		s.APITokens = kept
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}

func (a *App) restartXray(w http.ResponseWriter, r *http.Request) {
	if err := a.xr.Restart(); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) getXrayConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := xray.BuildConfig(a.st.Snapshot())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	reply(w, http.StatusOK, map[string]string{"config": string(raw)})
}

func (a *App) getXrayLogs(w http.ResponseWriter, r *http.Request) {
	logType := r.URL.Query().Get("type")
	level := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("level")))
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	file := "data/xray-error.log"
	if logType == "access" {
		file = "data/xray-access.log"
	}
	b, err := os.ReadFile(file)
	if err != nil {
		reply(w, http.StatusOK, map[string]any{
			"lines":   "لاگ ثبت نشده است. برای فعال‌سازی، گزینه ثبت لاگ Xray را در تنظیمات روشن کنید.",
			"total":   0,
			"matched": 0,
		})
		return
	}

	rawLines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(rawLines) > 400 {
		rawLines = rawLines[len(rawLines)-400:]
	}

	filtered := make([]string, 0, len(rawLines))
	for _, ln := range rawLines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		upper := strings.ToUpper(ln)
		if level != "" && level != "ALL" {
			switch level {
			case "ERROR", "ERR":
				if !strings.Contains(upper, "[ERROR]") && !strings.Contains(upper, " ERROR ") && !strings.Contains(upper, "FAILED") {
					continue
				}
			case "WARNING", "WARN":
				if !strings.Contains(upper, "[WARNING]") && !strings.Contains(upper, " WARN ") && !strings.Contains(upper, "WARNING") {
					continue
				}
			case "INFO":
				if !strings.Contains(upper, "[INFO]") && !strings.Contains(upper, " INFO ") {
					continue
				}
			}
		}
		if query != "" && !strings.Contains(strings.ToLower(ln), query) {
			continue
		}
		filtered = append(filtered, ln)
	}

	if len(filtered) > 160 {
		filtered = filtered[len(filtered)-160:]
	}

	reply(w, http.StatusOK, map[string]any{
		"lines":   strings.Join(filtered, "\n"),
		"total":   len(rawLines),
		"matched": len(filtered),
	})
}

func downloadFile(urlStr, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Get(urlStr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, dest)
}

func (a *App) updateGeoData(w http.ResponseWriter, r *http.Request) {
	binDir := filepath.Dir(a.cfg.XrayBinary)
	if binDir == "." {
		binDir = "/usr/local/share/xray"
	}
	geoipURL := "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat"
	geositeURL := "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat"
	if err := downloadFile(geoipURL, filepath.Join(binDir, "geoip.dat")); err != nil {
		fail(w, http.StatusInternalServerError, "geoip download failed: "+err.Error())
		return
	}
	if err := downloadFile(geositeURL, filepath.Join(binDir, "geosite.dat")); err != nil {
		fail(w, http.StatusInternalServerError, "geosite download failed: "+err.Error())
		return
	}
	reply(w, http.StatusOK, map[string]string{"dir": binDir})
}

func (a *App) backup(w http.ResponseWriter, r *http.Request) {
	st := a.st.Snapshot()
	delete(st.Settings, "admin_hash")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="fastasiapanel-backup.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(st)
}

func (a *App) restore(w http.ResponseWriter, r *http.Request) {
	var incoming store.State
	if !decode(w, r, &incoming) {
		return
	}
	err := a.st.Update(func(cur *store.State) error {
		adminHash := cur.Settings["admin_hash"]
		adminUser := cur.Settings["admin_user"]
		if incoming.Settings == nil {
			incoming.Settings = map[string]string{}
		}
		if adminHash != "" {
			incoming.Settings["admin_hash"] = adminHash
		}
		if adminUser != "" {
			incoming.Settings["admin_user"] = adminUser
		}
		*cur = incoming
		return nil
	})
	a.finish(w, err, map[string]bool{"ok": true})
}
