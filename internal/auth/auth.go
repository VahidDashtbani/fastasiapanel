package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"fastasiapanel/internal/store"
)

const (
	cookieName = "fap_session"
	sessionTTL = 12 * time.Hour
	maxFails   = 5
	lockFor    = 10 * time.Minute
	keyUser    = "admin_user"
	keyHash    = "admin_hash"
)

var (
	ErrBadLogin = errors.New("wrong username or password")
	ErrLocked   = errors.New("too many attempts, try again later")
)

type attempt struct {
	fails int
	until time.Time
	last  time.Time
}

type Manager struct {
	st       *store.Store
	secure   bool
	path     string
	mu       sync.Mutex
	sessions map[string]time.Time
	attempts map[string]*attempt
}

func New(st *store.Store, secure bool, cookiePath string) *Manager {
	if cookiePath == "" {
		cookiePath = "/"
	}
	return &Manager{st: st, secure: secure, path: cookiePath, sessions: map[string]time.Time{}, attempts: map[string]*attempt{}}
}

func (m *Manager) HasAdmin() bool {
	return m.st.Setting(keyHash) != ""
}

func (m *Manager) Username() string {
	return m.st.Setting(keyUser)
}

func (m *Manager) SetAdmin(user, pass string) error {
	if user == "" {
		return errors.New("username is required")
	}
	if len(pass) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	err = m.st.Update(func(s *store.State) error {
		s.Settings[keyUser] = user
		s.Settings[keyHash] = string(h)
		return nil
	})
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.sessions = map[string]time.Time{}
	m.mu.Unlock()
	return nil
}

func (m *Manager) ChangePassword(oldPass, newPass string) error {
	if bcrypt.CompareHashAndPassword([]byte(m.st.Setting(keyHash)), []byte(oldPass)) != nil {
		return errors.New("current password is wrong")
	}
	return m.SetAdmin(m.st.Setting(keyUser), newPass)
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func SameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (m *Manager) recordFail(ip string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.attempts[ip]
	if a == nil {
		a = &attempt{}
		m.attempts[ip] = a
	}
	a.fails++
	a.last = now
	if a.fails >= maxFails {
		a.fails = 0
		a.until = now.Add(lockFor)
	}
}

func (m *Manager) pruneLocked(now time.Time) {
	for t, exp := range m.sessions {
		if now.After(exp) {
			delete(m.sessions, t)
		}
	}
	if len(m.attempts) > 1000 {
		for ip, a := range m.attempts {
			if now.Sub(a.last) > lockFor {
				delete(m.attempts, ip)
			}
		}
	}
}

func (m *Manager) Login(w http.ResponseWriter, r *http.Request, user, pass string) error {
	ip := clientIP(r)
	now := time.Now()
	m.mu.Lock()
	if a := m.attempts[ip]; a != nil && now.Before(a.until) {
		m.mu.Unlock()
		return ErrLocked
	}
	m.mu.Unlock()

	hash := m.st.Setting(keyHash)
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(m.st.Setting(keyUser))) == 1
	passOK := hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(pass)) == nil
	if !userOK || !passOK {
		m.recordFail(ip, now)
		return ErrBadLogin
	}

	token, err := newToken()
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.attempts, ip)
	m.pruneLocked(now)
	m.sessions[token] = now.Add(sessionTTL)
	m.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: m.path, HttpOnly: true, Secure: m.secure,
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds()),
	})
	return nil
}

func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		m.mu.Lock()
		delete(m.sessions, c.Value)
		m.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: m.path, HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

func (m *Manager) Authed(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.sessions[c.Value]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(m.sessions, c.Value)
		return false
	}
	return true
}

func (m *Manager) Require(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.Authed(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !SameOrigin(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"cross-origin request blocked"}`))
			return
		}
		next(w, r)
	}
}
