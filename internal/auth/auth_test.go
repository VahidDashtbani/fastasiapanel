package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"fastasiapanel/internal/store"
)

func newManager(t *testing.T) *Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(st, false, "/")
	if err := m.SetAdmin("admin", "long enough pass"); err != nil {
		t.Fatal(err)
	}
	return m
}

func login(m *Manager, user, pass string) (*httptest.ResponseRecorder, error) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/login", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	return rec, m.Login(rec, req, user, pass)
}

func TestLoginAndLockout(t *testing.T) {
	m := newManager(t)
	rec, err := login(m, "admin", "long enough pass")
	if err != nil || len(rec.Result().Cookies()) != 1 {
		t.Fatalf("valid login failed: %v", err)
	}
	for i := 0; i < maxFails; i++ {
		if _, err := login(m, "admin", "wrong"); err != ErrBadLogin {
			t.Fatalf("attempt %d: expected bad login, got %v", i, err)
		}
	}
	if _, err := login(m, "admin", "long enough pass"); err != ErrLocked {
		t.Fatalf("expected lockout, got %v", err)
	}
}

func TestSessionRequiredAndOriginChecked(t *testing.T) {
	m := newManager(t)
	h := m.Require(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != 401 {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	lr, err := login(m, "admin", "long enough pass")
	if err != nil {
		t.Fatal(err)
	}
	ck := lr.Result().Cookies()[0]

	req := httptest.NewRequest("GET", "/x", nil)
	req.AddCookie(ck)
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 with session, got %d", rec.Code)
	}

	req = httptest.NewRequest("POST", "http://panel.local/x", nil)
	req.AddCookie(ck)
	req.Header.Set("Origin", "http://evil.example")
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != 403 {
		t.Fatalf("cross-origin post must be blocked, got %d", rec.Code)
	}
}

func TestShortPasswordRejected(t *testing.T) {
	m := newManager(t)
	if err := m.SetAdmin("admin", "short"); err == nil {
		t.Fatal("short passwords must be rejected")
	}
}
