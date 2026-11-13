package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Auth struct {
	secret   []byte
	basePath string
	secure   bool
	mu       sync.Mutex
	attempts map[string]attempt
}
type attempt struct {
	Count int
	Until time.Time
}
type claims struct {
	User    string `json:"u"`
	Expires int64  `json:"e"`
	Nonce   string `json:"n"`
}

func New(_ any, secret, base string, secure bool) *Auth {
	return &Auth{secret: []byte(secret), basePath: base, secure: secure, attempts: map[string]attempt{}}
}
func (a *Auth) LoginAllowed(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := a.attempts[ip]
	now := time.Now()
	if now.Before(v.Until) {
		return false
	}
	if v.Count >= 8 {
		delete(a.attempts, ip)
		return true
	}
	return true
}
func (a *Auth) Failed(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := a.attempts[ip]
	if time.Now().After(v.Until) {
		v = attempt{}
	}
	v.Count++
	if v.Count >= 8 {
		v.Until = time.Now().Add(10 * time.Minute)
	}
	a.attempts[ip] = v
}
func (a *Auth) Succeeded(ip string) { a.mu.Lock(); delete(a.attempts, ip); a.mu.Unlock() }
func (a *Auth) Set(w http.ResponseWriter, user string) {
	n := make([]byte, 16)
	_, _ = rand.Read(n)
	c := claims{User: user, Expires: time.Now().Add(12 * time.Hour).Unix(), Nonce: base64.RawURLEncoding.EncodeToString(n)}
	b, _ := json.Marshal(c)
	v := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(v))
	token := v + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{Name: "fap_session", Value: token, Path: cookiePath(a.basePath), HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
}
func (a *Auth) Valid(r *http.Request) bool {
	c, e := r.Cookie("fap_session")
	if e != nil {
		return false
	}
	p := strings.Split(c.Value, ".")
	if len(p) != 2 {
		return false
	}
	sig, e := base64.RawURLEncoding.DecodeString(p[1])
	if e != nil {
		return false
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(p[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return false
	}
	b, e := base64.RawURLEncoding.DecodeString(p[0])
	if e != nil {
		return false
	}
	var v claims
	if json.Unmarshal(b, &v) != nil || v.User == "" || time.Now().Unix() >= v.Expires {
		return false
	}
	return true
}
func (a *Auth) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "fap_session", Value: "", Path: cookiePath(a.basePath), HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}
func cookiePath(p string) string {
	if p == "" {
		return "/"
	}
	return p + "/"
}
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, ":")
	if len(parts) != 2 {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[0])
	if e != nil {
		return false
	}
	want, e := base64.RawStdEncoding.DecodeString(parts[1])
	if e != nil {
		return false
	}
	got := pbkdf2([]byte(password), salt, 210000, 32)
	return hmac.Equal(got, want)
}
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	sum := pbkdf2([]byte(password), salt, 210000, 32)
	return base64.RawStdEncoding.EncodeToString(salt) + ":" + base64.RawStdEncoding.EncodeToString(sum), nil
}
func pbkdf2(password, salt []byte, iter, n int) []byte {
	out := make([]byte, 0, n)
	for block := uint32(1); len(out) < n; block++ {
		m := hmac.New(sha256.New, password)
		m.Write(salt)
		m.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := m.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iter; i++ {
			m = hmac.New(sha256.New, password)
			m.Write(u)
			u = m.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:n]
}
