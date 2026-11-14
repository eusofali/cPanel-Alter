package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Time-based one-time passwords (RFC 6238): 6 digits, 30-second steps,
// HMAC-SHA1, which is what every authenticator app supports.

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func totpCode(secret []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000)
}

// matchTOTP returns the matching time step (allowing one step of clock
// drift either way), or 0 when the code is wrong.
func matchTOTP(secretB32, code string, now time.Time) uint64 {
	secret, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secretB32)))
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if err != nil || len(code) != 6 {
		return 0
	}
	step := uint64(now.Unix() / 30)
	for _, c := range []uint64{step, step - 1, step + 1} {
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, c)), []byte(code)) == 1 {
			return c
		}
	}
	return 0
}

// checkTOTP verifies a login code and refuses to accept the same code twice.
func (a *App) checkTOTP(code string) bool {
	step := matchTOTP(a.setting("totp_secret"), code, time.Now())
	if step == 0 {
		return false
	}
	last, _ := strconv.ParseUint(a.setting("totp_last"), 10, 64)
	if step <= last {
		return false
	}
	_ = a.setSetting("totp_last", strconv.FormatUint(step, 10))
	return true
}

// DisableTwoFactor is the recovery path used by the command line.
func (a *App) DisableTwoFactor() error {
	if err := a.setSetting("totp_enabled", "0"); err != nil {
		return err
	}
	return a.setSetting("totp_secret", "")
}

func (a *App) twoFactorStatus(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, map[string]bool{"enabled": a.setting("totp_enabled") == "1"})
	return nil
}

func (a *App) twoFactorSetup(w http.ResponseWriter, r *http.Request) error {
	if a.setting("totp_enabled") == "1" {
		return bad("two-factor sign-in is already on")
	}
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	secret := b32.EncodeToString(raw)
	if err := a.setSetting("totp_pending", secret); err != nil {
		return err
	}
	host, _ := os.Hostname()
	label := url.PathEscape("HMEA Panel:admin@" + host)
	uri := "otpauth://totp/" + label + "?secret=" + secret + "&issuer=" + url.QueryEscape("HMEA Panel") + "&digits=6&period=30"
	writeJSON(w, 200, map[string]string{"secret": secret, "uri": uri})
	return nil
}

func (a *App) twoFactorEnable(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	pending := a.setting("totp_pending")
	if pending == "" {
		return bad("start the setup again")
	}
	step := matchTOTP(pending, in.Code, time.Now())
	if step == 0 {
		return bad("that code is not valid; check that your phone's clock is correct and try the newest code")
	}
	_ = a.setSetting("totp_secret", pending)
	_ = a.setSetting("totp_pending", "")
	_ = a.setSetting("totp_last", strconv.FormatUint(step, 10))
	if err := a.setSetting("totp_enabled", "1"); err != nil {
		return err
	}
	a.audit("2fa.enabled", "two-factor sign-in turned on")
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) twoFactorDisable(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(a.setting("admin_hash")), []byte(in.Password)) != nil {
		return bad("password is wrong")
	}
	if err := a.DisableTwoFactor(); err != nil {
		return err
	}
	a.audit("2fa.disabled", "two-factor sign-in turned off")
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
