package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	cookieName  = "hostdeck_session"
	sessionTTL  = 12 * time.Hour
	maxFailures = 5
	failWindow  = 10 * time.Minute
)

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func hashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

func hashPassword(p string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(p), 12)
	return string(h), err
}

// SetAdminPassword stores a new admin password and signs everyone out.
func (a *App) SetAdminPassword(p string) error {
	if err := validPassword(p); err != nil {
		return err
	}
	h, err := hashPassword(p)
	if err != nil {
		return err
	}
	if err := a.setSetting("admin_hash", h); err != nil {
		return err
	}
	_, err = a.DB.Exec(`DELETE FROM sessions`)
	return err
}

// EnsureAdmin creates a random admin password on first start and returns it.
func (a *App) EnsureAdmin() (string, error) {
	if a.setting("admin_hash") != "" {
		return "", nil
	}
	p := randomToken(10)
	return p, a.SetAdminPassword(p)
}

func (a *App) authenticated(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return false
	}
	var expires int64
	if err := a.DB.QueryRow(`SELECT expires FROM sessions WHERE token=?`, hashToken(c.Value)).Scan(&expires); err != nil {
		return false
	}
	return expires > time.Now().Unix()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *App) throttled(ip string, record bool) bool {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	cutoff := time.Now().Add(-failWindow)
	var recent []time.Time
	for _, t := range a.failures[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if record {
		recent = append(recent, time.Now())
	}
	if len(recent) == 0 {
		delete(a.failures, ip)
	} else {
		a.failures[ip] = recent
	}
	return len(recent) >= maxFailures
}

func (a *App) me(w http.ResponseWriter, r *http.Request) error {
	host, _ := os.Hostname()
	writeJSON(w, 200, map[string]any{"authenticated": a.authenticated(r), "hostname": host, "dev": a.Sys.Dev})
	return nil
}

func (a *App) login(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	ip := clientIP(r)
	if a.throttled(ip, false) {
		return &HTTPError{http.StatusTooManyRequests, "too many failed attempts, try again in a few minutes"}
	}
	if bcrypt.CompareHashAndPassword([]byte(a.setting("admin_hash")), []byte(in.Password)) != nil {
		a.throttled(ip, true)
		a.audit("login.failed", "from "+ip)
		return &HTTPError{http.StatusUnauthorized, "wrong password"}
	}
	token := randomToken(32)
	now := time.Now()
	_, _ = a.DB.Exec(`DELETE FROM sessions WHERE expires < ?`, now.Unix())
	if _, err := a.DB.Exec(`INSERT INTO sessions(token,expires) VALUES(?,?)`, hashToken(token), now.Add(sessionTTL).Unix()); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.TLS,
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds()),
	})
	a.audit("login", "from "+ip)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(cookieName); err == nil {
		_, _ = a.DB.Exec(`DELETE FROM sessions WHERE token=?`, hashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(a.setting("admin_hash")), []byte(in.Current)) != nil {
		return bad("current password is wrong")
	}
	if err := a.SetAdminPassword(in.New); err != nil {
		return err
	}
	a.audit("password.changed", "admin password changed")
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
