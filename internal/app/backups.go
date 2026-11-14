package app

import (
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Backups are archives in backupDir:
//
//	full-<stamp>.tar.gz           every site folder, a dump of every database,
//	                              all mailboxes, and a copy of the panel database
//	site-<domain>-<stamp>.tar.gz  one site's folder
//	db-<name>-<stamp>.sql.gz      one database dump
//
// Only files in this directory can be restored. To move to another server,
// copy the archive into the same directory there; it is picked up on the
// next visit to the Backups page.
const backupDir = "/var/backups/hmea-panel"

var reBackupFile = regexp.MustCompile(`^(full|site-([a-z0-9.-]+)|db-([A-Za-z0-9_]+))-(\d{8}-\d{6})\.(tar|sql)\.gz$`)

type backup struct {
	ID      int64  `json:"id"`
	Kind    string `json:"kind"` // full | site | database
	Target  string `json:"target"`
	File    string `json:"file"`
	Size    int64  `json:"size"`
	Created int64  `json:"created"`
	Status  string `json:"status"` // running | done | failed | restoring
	Note    string `json:"note"`
	Auto    bool   `json:"auto"`
}

func (a *App) loadBackups() ([]backup, error) {
	rows, err := a.DB.Query(`SELECT id, kind, target, file, size, created, status, note, auto FROM backups ORDER BY created DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []backup{}
	for rows.Next() {
		var b backup
		if err := rows.Scan(&b.ID, &b.Kind, &b.Target, &b.File, &b.Size, &b.Created, &b.Status, &b.Note, &b.Auto); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (a *App) loadBackup(id int64) (backup, error) {
	var b backup
	err := a.DB.QueryRow(`SELECT id, kind, target, file, size, created, status, note, auto FROM backups WHERE id=?`, id).
		Scan(&b.ID, &b.Kind, &b.Target, &b.File, &b.Size, &b.Created, &b.Status, &b.Note, &b.Auto)
	if err != nil {
		return b, notFound("backup")
	}
	return b, nil
}

// adoptBackups registers archives that were copied into the directory by
// hand (for example from another server).
func (a *App) adoptBackups() {
	entries, err := os.ReadDir(a.Sys.Path(backupDir))
	if err != nil {
		return
	}
	for _, e := range entries {
		m := reBackupFile.FindStringSubmatch(e.Name())
		info, ierr := e.Info()
		if m == nil || ierr != nil || !info.Mode().IsRegular() {
			continue
		}
		kind, target := "full", ""
		if m[2] != "" {
			kind, target = "site", m[2]
		} else if m[3] != "" {
			kind, target = "database", m[3]
		}
		created := info.ModTime().Unix()
		if t, err := time.ParseInLocation("20060102-150405", m[4], time.Local); err == nil {
			created = t.Unix()
		}
		_, _ = a.DB.Exec(`INSERT OR IGNORE INTO backups(kind, target, file, size, created, status, note) VALUES(?,?,?,?,?, 'done', 'Found in the backup folder')`,
			kind, target, e.Name(), info.Size(), created)
	}
}

// claimJob lets only one backup or restore run at a time.
func (a *App) claimJob() bool {
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
	if a.jobBusy {
		return false
	}
	a.jobBusy = true
	return true
}

func (a *App) releaseJob() {
	a.jobMu.Lock()
	a.jobBusy = false
	a.jobMu.Unlock()
}

func (a *App) dbNames() ([]string, error) {
	rows, err := a.DB.Query(`SELECT name FROM dbs ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (a *App) dump(name, file string) error {
	_, err := a.Sys.RunTimeout(2*time.Hour, "", "mariadb-dump", "--single-transaction", "--routines", "--triggers", "--result-file="+file, name)
	return err
}

// startBackup validates the request, records it and runs it in the
// background. wait is closed when the archive is finished.
func (a *App) startBackup(kind, target string, auto bool) (int64, <-chan struct{}, error) {
	switch kind {
	case "full":
		target = ""
	case "site":
		var n int
		_ = a.DB.QueryRow(`SELECT COUNT(*) FROM sites WHERE domain=?`, target).Scan(&n)
		if n == 0 {
			return 0, nil, bad("choose a website to back up")
		}
	case "database":
		if !reDBName.MatchString(target) || !a.dbExists(target) {
			return 0, nil, bad("choose a database to back up")
		}
	default:
		return 0, nil, bad("backup type must be full, site or database")
	}
	if !a.claimJob() {
		return 0, nil, conflict("another backup or restore is still running")
	}
	now := time.Now()
	stamp := now.Format("20060102-150405")
	file := "full-" + stamp + ".tar.gz"
	if kind == "site" {
		file = "site-" + target + "-" + stamp + ".tar.gz"
	} else if kind == "database" {
		file = "db-" + target + "-" + stamp + ".sql.gz"
	}
	res, err := a.DB.Exec(`INSERT INTO backups(kind, target, file, created, status, auto) VALUES(?,?,?,?, 'running', ?)`, kind, target, file, now.Unix(), auto)
	if err != nil {
		a.releaseJob()
		if isUnique(err) {
			return 0, nil, conflict("a backup with this name was just created; wait a second and try again")
		}
		return 0, nil, err
	}
	id, _ := res.LastInsertId()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer a.releaseJob()
		err := a.runBackup(id, kind, target, file)
		status, note := "done", ""
		if err != nil {
			status, note = "failed", err.Error()
			_ = a.Sys.Remove(backupDir + "/" + file)
		}
		var size int64
		if info, serr := os.Stat(a.Sys.Path(backupDir + "/" + file)); serr == nil {
			size = info.Size()
		}
		_, _ = a.DB.Exec(`UPDATE backups SET status=?, note=?, size=? WHERE id=?`, status, note, size, id)
		what := kind
		if target != "" {
			what += " " + target
		}
		a.audit("backup."+status, what)
	}()
	return id, done, nil
}

func (a *App) runBackup(id int64, kind, target, file string) error {
	if err := a.Sys.MkdirAll(backupDir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(a.Sys.Path(backupDir), 0o700)
	out := backupDir + "/" + file
	var err error
	switch kind {
	case "site":
		_, err = a.Sys.RunTimeout(6*time.Hour, "", "tar", "-czf", out, "-C", WebRoot, target)
	case "database":
		sql := strings.TrimSuffix(out, ".gz")
		if err = a.dump(target, sql); err == nil {
			_, err = a.Sys.RunTimeout(2*time.Hour, "", "gzip", "-f", sql)
		}
	case "full":
		stage := fmt.Sprintf("%s/.stage-%d", backupDir, id)
		defer a.Sys.RemoveAll(stage)
		if err = a.Sys.MkdirAll(stage+"/databases", 0o700); err != nil {
			return err
		}
		if err = a.Sys.MkdirAll(stage+"/panel", 0o700); err != nil {
			return err
		}
		names, nerr := a.dbNames()
		if nerr != nil {
			return nerr
		}
		for _, n := range names {
			if err = a.dump(n, stage+"/databases/"+n+".sql"); err != nil {
				return err
			}
		}
		// A consistent copy of the panel's own database.
		if _, err = a.DB.Exec(`VACUUM INTO ?`, a.Sys.Path(stage+"/panel/hmea-panel.db")); err != nil {
			return err
		}
		args := []string{"-czf", out, "-C", stage, "databases", "panel", "-C", "/var", "www"}
		if a.Sys.Exists(mailBase) {
			args = append(args, "-C", "/var/mail", "vhosts")
		}
		_, err = a.Sys.RunTimeout(12*time.Hour, "", "tar", args...)
	}
	if err != nil {
		return err
	}
	if a.Sys.Dev {
		// Commands are simulated in dev mode; leave a stand-in so the UI has a file.
		return a.Sys.WriteFile(out, []byte("simulated backup\n"), 0o600)
	}
	return os.Chmod(out, 0o600)
}

// ensureDatabase creates a database that a backup refers to but the panel
// does not have (restoring onto a new server).
func (a *App) ensureDatabase(name string) error {
	if a.dbExists(name) {
		return nil
	}
	if _, err := a.sql("CREATE DATABASE IF NOT EXISTS `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		return err
	}
	_, err := a.DB.Exec(`INSERT OR IGNORE INTO dbs(name, created) VALUES(?,?)`, name, time.Now().Unix())
	return err
}

func (a *App) importSQL(name, file string, gz bool) error {
	if a.Sys.Dev {
		return a.Sys.RunIO(time.Minute, nil, "mariadb", name)
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	if !gz {
		return a.Sys.RunIO(6*time.Hour, f, "mariadb", name)
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("the backup file is not a valid gzip archive")
	}
	defer zr.Close()
	return a.Sys.RunIO(6*time.Hour, zr, "mariadb", name)
}

type restoreOptions struct {
	Files     bool `json:"files"`
	Databases bool `json:"databases"`
	Mail      bool `json:"mail"`
}

// runRestore puts a backup's contents back. Existing files with the same
// name are overwritten; files that are not in the backup are left alone.
func (a *App) runRestore(b backup, opt restoreOptions) error {
	archive := backupDir + "/" + b.File
	switch b.Kind {
	case "site":
		if _, err := a.Sys.RunTimeout(6*time.Hour, "", "tar", "-xzf", archive, "-C", WebRoot, "--no-same-owner", b.Target); err != nil {
			return err
		}
		if _, err := a.Sys.Run("", "chown", "-R", "www-data:www-data", siteDir(b.Target)); err != nil {
			return err
		}
		a.restartApps(b.Target)
	case "database":
		if err := a.ensureDatabase(b.Target); err != nil {
			return err
		}
		return a.importSQL(b.Target, a.Sys.Path(archive), true)
	case "full":
		if opt.Files {
			if _, err := a.Sys.RunTimeout(12*time.Hour, "", "tar", "-xzf", archive, "-C", "/var", "--no-same-owner", "www"); err != nil {
				return err
			}
			if _, err := a.Sys.Run("", "chown", "-R", "www-data:www-data", WebRoot); err != nil {
				return err
			}
			a.restartApps("")
		}
		if opt.Databases {
			stage := fmt.Sprintf("%s/.restore-%d", backupDir, b.ID)
			defer a.Sys.RemoveAll(stage)
			if err := a.Sys.MkdirAll(stage, 0o700); err != nil {
				return err
			}
			if _, err := a.Sys.RunTimeout(6*time.Hour, "", "tar", "-xzf", archive, "-C", stage, "--no-same-owner", "databases"); err != nil {
				return err
			}
			dumps, _ := filepath.Glob(a.Sys.Path(stage + "/databases/*.sql"))
			for _, d := range dumps {
				name := strings.TrimSuffix(filepath.Base(d), ".sql")
				if info, err := os.Lstat(d); err != nil || !info.Mode().IsRegular() || !reDBName.MatchString(name) || systemDBs[strings.ToLower(name)] {
					continue
				}
				if err := a.ensureDatabase(name); err != nil {
					return err
				}
				if err := a.importSQL(name, d, false); err != nil {
					return fmt.Errorf("database %s: %w", name, err)
				}
			}
		}
		if opt.Mail {
			if _, err := a.Sys.RunTimeout(12*time.Hour, "", "tar", "-xzf", archive, "-C", "/var/mail", "--no-same-owner", "vhosts"); err != nil {
				return err
			}
			if _, err := a.Sys.Run("", "chown", "-R", "vmail:vmail", mailBase); err != nil {
				return err
			}
		}
	}
	return nil
}

// restartApps restarts the Node.js/Python service of one site ("" = all).
func (a *App) restartApps(domain string) {
	sites, _ := a.loadSites()
	for _, s := range sites {
		if s.isApp() && s.Enabled && (domain == "" || s.Domain == domain) {
			_, _ = a.Sys.Run("", "systemctl", "restart", unitName(s.ID))
		}
	}
}

func (a *App) backupSettings() (string, int) {
	schedule := a.setting("backup_schedule")
	if schedule != "daily" && schedule != "weekly" {
		schedule = "off"
	}
	keep, _ := strconv.Atoi(a.setting("backup_keep"))
	if keep < 1 {
		keep = 7
	}
	return schedule, keep
}

func (a *App) listBackups(w http.ResponseWriter, r *http.Request) error {
	a.adoptBackups()
	list, err := a.loadBackups()
	if err != nil {
		return err
	}
	sites, err := a.loadSites()
	if err != nil {
		return err
	}
	domains := []string{}
	for _, s := range sites {
		domains = append(domains, s.Domain)
	}
	dbs, err := a.dbNames()
	if err != nil {
		return err
	}
	schedule, keep := a.backupSettings()
	var st syscall.Statfs_t
	_ = a.Sys.MkdirAll(backupDir, 0o700)
	_ = syscall.Statfs(a.Sys.Path(backupDir), &st)
	writeJSON(w, 200, map[string]any{
		"backups": list, "sites": domains, "databases": dbs, "schedule": schedule, "keep": keep,
		"dir": backupDir, "free": st.Bavail * uint64(st.Bsize),
	})
	return nil
}

func (a *App) createBackup(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Kind   string `json:"kind"`
		Target string `json:"target"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	id, _, err := a.startBackup(in.Kind, in.Target, false)
	if err != nil {
		return err
	}
	writeJSON(w, 202, map[string]int64{"id": id})
	return nil
}

func (a *App) restoreBackup(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var opt restoreOptions
	if err := readJSON(r, &opt); err != nil {
		return err
	}
	b, err := a.loadBackup(id)
	if err != nil {
		return err
	}
	if b.Status != "done" {
		return bad("this backup is not ready to restore")
	}
	if !reBackupFile.MatchString(b.File) || !a.Sys.Exists(backupDir+"/"+b.File) {
		return bad("the backup file is missing from %s", backupDir)
	}
	if b.Kind == "full" && !opt.Files && !opt.Databases && !opt.Mail {
		return bad("choose at least one thing to restore")
	}
	if b.Kind == "site" {
		var n int
		_ = a.DB.QueryRow(`SELECT COUNT(*) FROM sites WHERE domain=?`, b.Target).Scan(&n)
		if n == 0 {
			return bad("add the website %s first, then restore its files", b.Target)
		}
	}
	if !a.claimJob() {
		return conflict("another backup or restore is still running")
	}
	_, _ = a.DB.Exec(`UPDATE backups SET status='restoring', note='' WHERE id=?`, id)
	go func() {
		defer a.releaseJob()
		note := "Restored " + time.Now().Format("2006-01-02 15:04")
		result := "restored"
		if err := a.runRestore(b, opt); err != nil {
			note, result = "Restore failed: "+err.Error(), "restore-failed"
		}
		_, _ = a.DB.Exec(`UPDATE backups SET status='done', note=? WHERE id=?`, note, id)
		a.audit("backup."+result, b.File)
	}()
	writeJSON(w, 202, map[string]bool{"ok": true})
	return nil
}

func (a *App) downloadBackup(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	b, err := a.loadBackup(id)
	if err != nil {
		return err
	}
	if b.Status == "running" || !reBackupFile.MatchString(b.File) {
		return bad("this backup is not ready")
	}
	f, err := os.Open(a.Sys.Path(backupDir + "/" + b.File))
	if err != nil {
		return notFound("backup file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return notFound("backup file")
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlEscape(b.File))
	http.ServeContent(w, r, "", info.ModTime(), f)
	a.audit("backup.downloaded", b.File)
	return nil
}

func (a *App) removeBackup(b backup) error {
	if reBackupFile.MatchString(b.File) {
		if err := a.Sys.Remove(backupDir + "/" + b.File); err != nil {
			return err
		}
	}
	_, err := a.DB.Exec(`DELETE FROM backups WHERE id=?`, b.ID)
	return err
}

func (a *App) deleteBackup(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	b, err := a.loadBackup(id)
	if err != nil {
		return err
	}
	if b.Status == "running" || b.Status == "restoring" {
		return bad("wait until this backup has finished")
	}
	if err := a.removeBackup(b); err != nil {
		return err
	}
	a.audit("backup.deleted", b.File)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) setBackupSchedule(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Schedule string `json:"schedule"`
		Keep     int    `json:"keep"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Schedule != "off" && in.Schedule != "daily" && in.Schedule != "weekly" {
		return bad("schedule must be off, daily or weekly")
	}
	if in.Keep < 1 || in.Keep > 365 {
		return bad("keep between 1 and 365 automatic backups")
	}
	_ = a.setSetting("backup_schedule", in.Schedule)
	if err := a.setSetting("backup_keep", strconv.Itoa(in.Keep)); err != nil {
		return err
	}
	a.audit("backup.schedule", fmt.Sprintf("%s, keep %d", in.Schedule, in.Keep))
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

// scheduledBackup makes an automatic full backup when one is due and then
// deletes automatic backups beyond the retention count.
func (a *App) scheduledBackup(now time.Time) {
	schedule, keep := a.backupSettings()
	if schedule == "off" {
		return
	}
	period := 24 * time.Hour
	if schedule == "weekly" {
		period = 7 * 24 * time.Hour
	}
	var last int64
	_ = a.DB.QueryRow(`SELECT COALESCE(MAX(created), 0) FROM backups WHERE auto=1 AND kind='full'`).Scan(&last)
	if now.Sub(time.Unix(last, 0)) < period-30*time.Minute {
		return
	}
	_, done, err := a.startBackup("full", "", true)
	if err != nil {
		return
	}
	<-done
	list, err := a.loadBackups()
	if err != nil {
		return
	}
	kept := 0
	for _, b := range list { // newest first
		if !b.Auto || b.Kind != "full" || b.Status != "done" {
			continue
		}
		if kept++; kept > keep {
			_ = a.removeBackup(b)
		}
	}
}

// RunScheduler runs background jobs until ctx is cancelled.
func (a *App) RunScheduler(ctx context.Context) {
	// A backup interrupted by a restart can never finish.
	_, _ = a.DB.Exec(`UPDATE backups SET status='failed', note='Interrupted by a panel restart' WHERE status='running'`)
	_, _ = a.DB.Exec(`UPDATE backups SET status='done', note='Restore was interrupted by a panel restart' WHERE status='restoring'`)
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			a.scheduledBackup(now)
		}
	}
}

// siteLogs returns the end of a site's Nginx access or error log.
func (a *App) siteLogs(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	s, err := a.loadSite(id)
	if err != nil {
		return err
	}
	kind := r.URL.Query().Get("kind")
	if kind != "error" {
		kind = "access"
	}
	const max = 96 << 10
	text := ""
	if f, err := os.Open(a.Sys.Path("/var/log/nginx/" + s.Domain + "." + kind + ".log")); err == nil {
		defer f.Close()
		if info, err := f.Stat(); err == nil {
			start := info.Size() - max
			if start < 0 {
				start = 0
			}
			buf := make([]byte, info.Size()-start)
			n, _ := f.ReadAt(buf, start)
			text = string(buf[:n])
			if start > 0 { // drop the partial first line
				if i := strings.IndexByte(text, '\n'); i >= 0 {
					text = text[i+1:]
				}
			}
		}
	}
	writeJSON(w, 200, map[string]string{"kind": kind, "log": strings.ToValidUTF8(text, "?")})
	return nil
}
