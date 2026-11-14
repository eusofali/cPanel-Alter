// Package app implements the hmea-panel HTTP API and the services behind it.
package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"hmeapanel/internal/sys"
)

const WebRoot = "/var/www"

type App struct {
	DB  *sql.DB
	Sys *sys.Sys
	Log *slog.Logger
	UI  fs.FS // built frontend, may be nil
	TLS bool

	mu       sync.Mutex // serialises config-changing operations
	loginMu  sync.Mutex
	failures map[string][]time.Time
	devExt   map[string]bool // simulated installed extensions in dev mode
	jobMu    sync.Mutex
	jobBusy  bool // a backup or restore is running
}

type HTTPError struct {
	Status int
	Msg    string
}

func (e *HTTPError) Error() string { return e.Msg }

func bad(format string, args ...any) error {
	return &HTTPError{http.StatusBadRequest, fmt.Sprintf(format, args...)}
}

func notFound(what string) error { return &HTTPError{http.StatusNotFound, what + " not found"} }

func conflict(format string, args ...any) error {
	return &HTTPError{http.StatusConflict, fmt.Sprintf(format, args...)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 4<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return bad("invalid request body")
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, bad("invalid id")
	}
	return id, nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

type handler func(w http.ResponseWriter, r *http.Request) error

// wrap adds CSRF protection, authentication and JSON error rendering.
func (a *App) wrap(public bool, fn handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A custom header cannot be sent cross-site without a CORS preflight,
		// which we never approve; this blocks CSRF on state-changing requests.
		if r.Method != http.MethodGet && r.Header.Get("X-Requested-With") != "hmea-panel" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "missing X-Requested-With header"})
			return
		}
		if !public && !a.authenticated(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
			return
		}
		if err := fn(w, r); err != nil {
			var he *HTTPError
			if errors.As(err, &he) {
				writeJSON(w, he.Status, map[string]string{"error": he.Msg})
				return
			}
			a.Log.Error("request failed", "path", r.URL.Path, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	})
}

func (a *App) Handler() http.Handler {
	a.failures = map[string][]time.Time{}
	mux := http.NewServeMux()
	pub := func(p string, fn handler) { mux.Handle(p, a.wrap(true, fn)) }
	api := func(p string, fn handler) { mux.Handle(p, a.wrap(false, fn)) }

	pub("GET /api/me", a.me)
	pub("POST /api/login", a.login)
	pub("POST /api/logout", a.logout)
	api("POST /api/password", a.changePassword)

	api("GET /api/system", a.systemInfo)
	api("POST /api/services/{name}/{action}", a.serviceAction)
	api("GET /api/settings", a.getSettings)
	api("PUT /api/settings", a.putSettings)

	api("GET /api/sites", a.listSites)
	api("POST /api/sites", a.createSite)
	api("PUT /api/sites/{id}", a.updateSite)
	api("DELETE /api/sites/{id}", a.deleteSite)
	api("POST /api/sites/{id}/ssl", a.issueSSL)
	api("DELETE /api/sites/{id}/ssl", a.disableSSL)
	api("POST /api/sites/{id}/app/restart", a.restartApp)
	api("POST /api/sites/{id}/app/install", a.installAppDeps)
	api("GET /api/sites/{id}/app/logs", a.appLogs)

	api("GET /api/sites/{id}/logs", a.siteLogs)

	api("GET /api/backups", a.listBackups)
	api("POST /api/backups", a.createBackup)
	api("PUT /api/backups/schedule", a.setBackupSchedule)
	api("POST /api/backups/{id}/restore", a.restoreBackup)
	api("GET /api/backups/{id}/download", a.downloadBackup)
	api("DELETE /api/backups/{id}", a.deleteBackup)

	api("GET /api/cron", a.listCron)
	api("POST /api/cron", a.createCron)
	api("PUT /api/cron/{id}", a.updateCron)
	api("DELETE /api/cron/{id}", a.deleteCron)

	api("GET /api/2fa", a.twoFactorStatus)
	api("POST /api/2fa/setup", a.twoFactorSetup)
	api("POST /api/2fa/enable", a.twoFactorEnable)
	api("POST /api/2fa/disable", a.twoFactorDisable)

	api("GET /api/php", a.listPHP)
	api("POST /api/php/{version}/extensions", a.setPHPExtension)

	api("GET /api/databases", a.listDatabases)
	api("POST /api/databases", a.createDatabase)
	api("DELETE /api/databases/{name}", a.deleteDatabase)
	api("POST /api/dbusers", a.createDBUser)
	api("PUT /api/dbusers/{name}", a.updateDBUser)
	api("DELETE /api/dbusers/{name}", a.deleteDBUser)

	api("GET /api/files", a.listFiles)
	api("GET /api/files/content", a.readFile)
	api("PUT /api/files/content", a.writeFile)
	api("GET /api/files/download", a.downloadFile)
	api("POST /api/files/upload", a.uploadFile)
	api("POST /api/files/mkdir", a.mkdir)
	api("POST /api/files/rename", a.renameFile)
	api("DELETE /api/files", a.deleteFile)

	api("GET /api/ftp", a.listFTP)
	api("POST /api/ftp", a.createFTP)
	api("PUT /api/ftp/{id}", a.updateFTP)
	api("DELETE /api/ftp/{id}", a.deleteFTP)

	api("GET /api/mail", a.listMail)
	api("POST /api/mail/domains", a.createMailDomain)
	api("DELETE /api/mail/domains/{id}", a.deleteMailDomain)
	api("POST /api/mail/mailboxes", a.createMailbox)
	api("PUT /api/mail/mailboxes/{id}", a.updateMailbox)
	api("DELETE /api/mail/mailboxes/{id}", a.deleteMailbox)
	api("POST /api/mail/aliases", a.createAlias)
	api("DELETE /api/mail/aliases/{id}", a.deleteAlias)

	api("GET /api/dns/zones", a.listZones)
	api("POST /api/dns/zones", a.createZone)
	api("GET /api/dns/zones/{id}", a.getZone)
	api("DELETE /api/dns/zones/{id}", a.deleteZone)
	api("POST /api/dns/zones/{id}/records", a.createRecord)
	api("PUT /api/dns/records/{id}", a.updateRecord)
	api("DELETE /api/dns/records/{id}", a.deleteRecord)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown endpoint"})
	})
	if a.UI != nil {
		mux.Handle("/", http.FileServerFS(a.UI))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		mux.ServeHTTP(w, r)
	})
}
