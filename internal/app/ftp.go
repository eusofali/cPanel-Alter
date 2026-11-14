package app

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

const (
	ftpPasswd = "/etc/pure-ftpd/pureftpd.passwd"
	ftpPDB    = "/etc/pure-ftpd/pureftpd.pdb"
)

type ftpAccount struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Home     string `json:"home"` // relative to the web root
	Created  int64  `json:"created"`
}

// applyFTP rewrites the pure-ftpd virtual user file from the database.
// Every account maps to www-data and is chrooted ("/./") to its home.
func (a *App) applyFTP() error {
	rows, err := a.DB.Query(`SELECT username, hash, home FROM ftp ORDER BY username`)
	if err != nil {
		return err
	}
	uid, gid := a.Sys.WebUser()
	var b strings.Builder
	for rows.Next() {
		var user, hash, home string
		if err := rows.Scan(&user, &hash, &home); err != nil {
			rows.Close()
			return err
		}
		fmt.Fprintf(&b, "%s:%s:%d:%d::%s/./::::::::::::\n", user, hash, uid, gid, path.Join(WebRoot, home))
	}
	rows.Close()
	if err := a.Sys.WriteFile(ftpPasswd, []byte(b.String()), 0o600); err != nil {
		return err
	}
	if _, err := a.Sys.Run("", "pure-pw", "mkdb", ftpPDB, "-f", ftpPasswd); err != nil {
		return bad("%v", err)
	}
	return nil
}

func (a *App) listFTP(w http.ResponseWriter, r *http.Request) error {
	rows, err := a.DB.Query(`SELECT id, username, home, created FROM ftp ORDER BY username`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []ftpAccount{}
	for rows.Next() {
		var f ftpAccount
		if err := rows.Scan(&f.ID, &f.Username, &f.Home, &f.Created); err != nil {
			return err
		}
		out = append(out, f)
	}
	writeJSON(w, 200, map[string]any{"accounts": out})
	return nil
}

// ftpHome validates a home directory: it must already exist inside the web
// root and may not be the web root itself (that would expose every site).
func (a *App) ftpHome(rel string) (string, error) {
	clean := path.Clean("/" + rel)
	if clean == "/" {
		return "", bad("choose a site folder as the home directory, not the whole web root")
	}
	if strings.ContainsAny(clean, ":\n\r") {
		return "", bad("home directory contains unsupported characters")
	}
	p, err := a.resolve(clean)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(p); err != nil || !info.IsDir() {
		return "", bad("home directory %s does not exist", clean)
	}
	return clean, nil
}

func (a *App) createFTP(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Home     string `json:"home"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !reUser.MatchString(in.Username) {
		return bad("username must start with a letter and use only a-z, 0-9, - and _ (2-32 characters)")
	}
	if err := validPassword(in.Password); err != nil {
		return err
	}
	home, err := a.ftpHome(in.Home)
	if err != nil {
		return err
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	res, err := a.DB.Exec(`INSERT INTO ftp(username, hash, home, created) VALUES(?,?,?,?)`, in.Username, hash, home, time.Now().Unix())
	if isUnique(err) {
		return conflict("FTP account %s already exists", in.Username)
	} else if err != nil {
		return err
	}
	if err := a.applyFTP(); err != nil {
		id, _ := res.LastInsertId()
		_, _ = a.DB.Exec(`DELETE FROM ftp WHERE id=?`, id)
		return err
	}
	a.audit("ftp.created", in.Username+" → "+home)
	writeJSON(w, 201, map[string]bool{"ok": true})
	return nil
}

func (a *App) updateFTP(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct {
		Password string `json:"password"`
		Home     string `json:"home"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	home, err := a.ftpHome(in.Home)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var user string
	if a.DB.QueryRow(`SELECT username FROM ftp WHERE id=?`, id).Scan(&user) != nil {
		return notFound("FTP account")
	}
	if in.Password != "" {
		if err := validPassword(in.Password); err != nil {
			return err
		}
		hash, err := hashPassword(in.Password)
		if err != nil {
			return err
		}
		if _, err := a.DB.Exec(`UPDATE ftp SET hash=? WHERE id=?`, hash, id); err != nil {
			return err
		}
	}
	if _, err := a.DB.Exec(`UPDATE ftp SET home=? WHERE id=?`, home, id); err != nil {
		return err
	}
	if err := a.applyFTP(); err != nil {
		return err
	}
	a.audit("ftp.updated", user)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) deleteFTP(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var user string
	if a.DB.QueryRow(`SELECT username FROM ftp WHERE id=?`, id).Scan(&user) != nil {
		return notFound("FTP account")
	}
	if _, err := a.DB.Exec(`DELETE FROM ftp WHERE id=?`, id); err != nil {
		return err
	}
	if err := a.applyFTP(); err != nil {
		return err
	}
	a.audit("ftp.deleted", user)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
