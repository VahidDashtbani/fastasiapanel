package web

import (
	"fmt"
	"time"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"fastasiapanel/internal/auth"
	"fastasiapanel/internal/config"
	"fastasiapanel/internal/store"
	"fastasiapanel/internal/xray"
)

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{BasePath: "/secret", DBType: "json", XrayBinary: "xray-missing", XrayConfig: filepath.Join(dir, "xray.json")}
	st, err := store.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	au := auth.New(st, false, cfg.BasePath)
	if err := au.SetAdmin("admin", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{
		"index.html": {Data: []byte("index")}, "login.html": {Data: []byte("login")},
		"app.js": {Data: []byte("js")}, "login.js": {Data: []byte("js")}, "style.css": {Data: []byte("css")},
	}
	return New(cfg, st, au, xray.NewManager(cfg, st), assets, "test").Handler()
}

func call(h http.Handler, method, path, body, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPanelFlow(t *testing.T) {
	h := newHandler(t)

	if c := call(h, "GET", "/", "", "").Code; c != 404 {
		t.Fatalf("root should be hidden, got %d", c)
	}
	if c := call(h, "GET", "/secret/api/inbounds", "", "").Code; c != 401 {
		t.Fatalf("unauthenticated api should be 401, got %d", c)
	}
	if c := call(h, "POST", "/secret/api/login", `{"username":"admin","password":"nope"}`, "").Code; c != 401 {
		t.Fatalf("bad login should be 401, got %d", c)
	}
	rec := call(h, "POST", "/secret/api/login", `{"username":"admin","password":"correct horse battery"}`, "")
	if rec.Code != 200 || len(rec.Result().Cookies()) == 0 {
		t.Fatalf("login failed: %d", rec.Code)
	}
	ck := rec.Result().Cookies()[0]
	cookie := ck.Name + "=" + ck.Value

	body := `{"remark":"r","protocol":"vless","port":443,"options":{"network":"tcp","security":"reality","reality":{"dest":"www.microsoft.com:443","server_name":"www.microsoft.com"}}}`
	rec = call(h, "POST", "/secret/api/inbounds", body, cookie)
	if rec.Code != 201 {
		t.Fatalf("create inbound: %d %s", rec.Code, rec.Body.String())
	}
	var created struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &created)

	if c := call(h, "POST", "/secret/api/inbounds", body, cookie).Code; c != 409 {
		t.Fatalf("duplicate port should be 409, got %d", c)
	}
	bad := `{"remark":"x","protocol":"shadowsocks","port":8443,"options":{"network":"tcp","security":"tls"}}`
	if c := call(h, "POST", "/secret/api/inbounds", bad, cookie).Code; c != 400 {
		t.Fatalf("invalid inbound should be 400, got %d", c)
	}

	cb := `{"inbound_id":"` + created.ID + `","email":"user1","total_bytes":1000,"flow":"xtls-rprx-vision"}`
	if c := call(h, "POST", "/secret/api/clients", cb, cookie); c.Code != 201 {
		t.Fatalf("create client: %d %s", c.Code, c.Body.String())
	}
	if c := call(h, "POST", "/secret/api/clients", cb, cookie).Code; c != 409 {
		t.Fatalf("duplicate email should be 409, got %d", c)
	}

	rec = call(h, "GET", "/secret/api/inbounds", "", cookie)
	var list []inboundView
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 || len(list[0].Clients) != 1 {
		t.Fatalf("unexpected list: %s", rec.Body.String())
	}
	link := list[0].Clients[0].Link
	for _, want := range []string{"vless://", "security=reality", "pbk=", "flow=xtls-rprx-vision", "#user1"} {
		if !strings.Contains(link, want) {
			t.Fatalf("link %q is missing %q", link, want)
		}
	}

	if c := call(h, "DELETE", "/secret/api/inbounds/"+created.ID, "", cookie).Code; c != 200 {
		t.Fatalf("delete inbound: %d", c)
	}
	rec = call(h, "GET", "/secret/api/inbounds", "", cookie)
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected empty list, got %s", rec.Body.String())
	}
}


// Auto-update check & trigger endpoints for FastAsiaPanel
type fapReleaseInfo struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

func fapFetchLatestRelease() (*fapReleaseInfo, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/VahidDashtbani/fastasiapanel/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "FastAsiaPanel-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github status %d", resp.StatusCode)
	}
	var rel fapReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}
