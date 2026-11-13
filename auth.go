package main

import (
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const cookieName = "fap_session"

func isAuthed(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	var s Session
	if DB.First(&s, "token = ?", c.Value).Error != nil {
		return false
	}
	if time.Now().After(s.ExpiresAt) {
		DB.Delete(&s)
		return false
	}
	return true
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAuthed(r) {
			fail(w, 401, "unauthorized")
			return
		}
		next(w, r)
	}
}

func loginPage(w http.ResponseWriter, r *http.Request) {
	data, _ := templateFS.ReadFile("templates/login.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	var req struct{ Username, Password string }
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		fail(w, 400, "bad request")
		return
	}
	var u User
	if DB.Where("username = ?", req.Username).First(&u).Error != nil ||
		bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.Password)) != nil {
		time.Sleep(time.Second) // slow down brute force
		fail(w, 401, "wrong username or password")
		return
	}
	tok := randHex(32)
	DB.Create(&Session{Token: tok, ExpiresAt: time.Now().Add(12 * time.Hour)})
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   12 * 3600,
	})
	out(w, map[string]bool{"ok": true})
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		DB.Delete(&Session{}, "token = ?", c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1})
	out(w, map[string]bool{"ok": true})
}

func passwordHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.New) < 8 {
		fail(w, 400, "new password must be at least 8 characters")
		return
	}
	var u User
	DB.First(&u)
	if bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.Old)) != nil {
		fail(w, 403, "old password is wrong")
		return
	}
	h, _ := bcrypt.GenerateFromPassword([]byte(req.New), bcrypt.DefaultCost)
	DB.Model(&u).Update("password", string(h))
	out(w, map[string]bool{"ok": true})
}