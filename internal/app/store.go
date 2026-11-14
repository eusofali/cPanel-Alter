package app

import (
	"database/sql"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY, expires INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS activity(id INTEGER PRIMARY KEY, at INTEGER NOT NULL, action TEXT NOT NULL, detail TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sites(
  id INTEGER PRIMARY KEY, domain TEXT UNIQUE NOT NULL, aliases TEXT NOT NULL DEFAULT '',
  type TEXT NOT NULL, php TEXT NOT NULL DEFAULT '', proxy TEXT NOT NULL DEFAULT '',
  ssl INTEGER NOT NULL DEFAULT 0, force_https INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1, created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS dbs(name TEXT PRIMARY KEY, created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS db_users(name TEXT PRIMARY KEY, created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS db_grants(user TEXT NOT NULL, db TEXT NOT NULL, PRIMARY KEY(user, db));
CREATE TABLE IF NOT EXISTS ftp(id INTEGER PRIMARY KEY, username TEXT UNIQUE NOT NULL, hash TEXT NOT NULL, home TEXT NOT NULL, created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS mail_domains(id INTEGER PRIMARY KEY, domain TEXT UNIQUE NOT NULL);
CREATE TABLE IF NOT EXISTS mailboxes(
  id INTEGER PRIMARY KEY, domain_id INTEGER NOT NULL REFERENCES mail_domains(id) ON DELETE CASCADE,
  local TEXT NOT NULL, hash TEXT NOT NULL, quota_mb INTEGER NOT NULL DEFAULT 1024, UNIQUE(domain_id, local));
CREATE TABLE IF NOT EXISTS mail_aliases(
  id INTEGER PRIMARY KEY, domain_id INTEGER NOT NULL REFERENCES mail_domains(id) ON DELETE CASCADE,
  source TEXT NOT NULL, destination TEXT NOT NULL, UNIQUE(domain_id, source));
CREATE TABLE IF NOT EXISTS backups(
  id INTEGER PRIMARY KEY, kind TEXT NOT NULL, target TEXT NOT NULL DEFAULT '', file TEXT UNIQUE NOT NULL,
  size INTEGER NOT NULL DEFAULT 0, created INTEGER NOT NULL, status TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '', auto INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS cron(
  id INTEGER PRIMARY KEY, schedule TEXT NOT NULL, command TEXT NOT NULL,
  comment TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1);
CREATE TABLE IF NOT EXISTS dns_zones(id INTEGER PRIMARY KEY, domain TEXT UNIQUE NOT NULL, serial INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS dns_records(
  id INTEGER PRIMARY KEY, zone_id INTEGER NOT NULL REFERENCES dns_zones(id) ON DELETE CASCADE,
  name TEXT NOT NULL, type TEXT NOT NULL, ttl INTEGER NOT NULL DEFAULT 3600,
  prio INTEGER NOT NULL DEFAULT 0, value TEXT NOT NULL);
`

func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// One connection keeps writes serialised; never run a query while rows are open.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	// Columns added after the first release; "duplicate column" means done.
	for _, m := range []string{
		`ALTER TABLE sites ADD COLUMN entry TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sites ADD COLUMN port INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func (a *App) setting(key string) string {
	var v string
	_ = a.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v
}

func (a *App) setSetting(key, value string) error {
	_, err := a.DB.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (a *App) audit(action, detail string) {
	_, _ = a.DB.Exec(`INSERT INTO activity(at,action,detail) VALUES(?,?,?)`, time.Now().Unix(), action, detail)
	_, _ = a.DB.Exec(`DELETE FROM activity WHERE id NOT IN (SELECT id FROM activity ORDER BY id DESC LIMIT 500)`)
}
