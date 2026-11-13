package auth

import (
	"net/http/httptest"
	"testing"
)

func TestSessionCookie(t *testing.T) {
	a := New(nil, "01234567890123456789012345678901", "/panel", true)
	w := httptest.NewRecorder()
	a.Set(w, "admin")
	res := w.Result()
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	r := httptest.NewRequest("GET", "https://example.test/panel/", nil)
	r.AddCookie(cookies[0])
	if !a.Valid(r) {
		t.Fatal("expected valid session")
	}
	cookies[0].Value += "x"
	r = httptest.NewRequest("GET", "https://example.test/panel/", nil)
	r.AddCookie(cookies[0])
	if a.Valid(r) {
		t.Fatal("tampered session was accepted")
	}
}
func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("correct horse battery staple", h) {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword("wrong", h) {
		t.Fatal("invalid password accepted")
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("a-long-test-password", hash) {
		t.Fatal("valid password was rejected")
	}
	if VerifyPassword("another-password", hash) {
		t.Fatal("invalid password was accepted")
	}
}
