package app

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

var systemDBs = map[string]bool{"mysql": true, "information_schema": true, "performance_schema": true, "sys": true}
var systemDBUsers = map[string]bool{"root": true, "mysql": true, "mariadb.sys": true, "debian-sys-maint": true}

// sqlString quotes a value as a MariaDB string literal.
func sqlString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// grantPattern escapes the wildcards GRANT would otherwise interpret in a
// database name, so `shop_1` cannot also match `shopA1`.
func grantPattern(db string) string {
	return "`" + strings.NewReplacer(`_`, `\_`, `%`, `\%`).Replace(db) + "`"
}

func (a *App) sql(statements ...string) (string, error) {
	return a.Sys.Run(strings.Join(statements, ";\n")+";\n", "mariadb", "--batch", "--skip-column-names")
}

type dbUser struct {
	Name      string   `json:"name"`
	Databases []string `json:"databases"`
}

type dbInfo struct {
	Name  string   `json:"name"`
	Users []string `json:"users"`
	Size  int64    `json:"size"`
}

func (a *App) dbExists(name string) bool {
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM dbs WHERE name=?`, name).Scan(&n)
	return n > 0
}

func (a *App) listDatabases(w http.ResponseWriter, r *http.Request) error {
	sizes := map[string]int64{}
	if out, err := a.sql("SELECT table_schema, COALESCE(SUM(data_length + index_length), 0) FROM information_schema.tables GROUP BY table_schema"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if f := strings.Fields(line); len(f) == 2 {
				sizes[f[0]], _ = strconv.ParseInt(strings.Split(f[1], ".")[0], 10, 64)
			}
		}
	}

	grants := map[string][]string{} // user -> dbs
	users := map[string][]string{}  // db -> users
	rows, err := a.DB.Query(`SELECT user, db FROM db_grants ORDER BY user, db`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var u, d string
		if err := rows.Scan(&u, &d); err != nil {
			rows.Close()
			return err
		}
		grants[u] = append(grants[u], d)
		users[d] = append(users[d], u)
	}
	rows.Close()

	dbs := []dbInfo{}
	rows, err = a.DB.Query(`SELECT name FROM dbs ORDER BY name`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d dbInfo
		if err := rows.Scan(&d.Name); err != nil {
			rows.Close()
			return err
		}
		d.Users, d.Size = users[d.Name], sizes[d.Name]
		if d.Users == nil {
			d.Users = []string{}
		}
		dbs = append(dbs, d)
	}
	rows.Close()

	us := []dbUser{}
	rows, err = a.DB.Query(`SELECT name FROM db_users ORDER BY name`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var u dbUser
		if err := rows.Scan(&u.Name); err != nil {
			rows.Close()
			return err
		}
		u.Databases = grants[u.Name]
		if u.Databases == nil {
			u.Databases = []string{}
		}
		us = append(us, u)
	}
	rows.Close()

	writeJSON(w, 200, map[string]any{"databases": dbs, "users": us})
	return nil
}

func (a *App) createDatabase(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !reDBName.MatchString(in.Name) || systemDBs[strings.ToLower(in.Name)] {
		return bad("database name may only contain letters, digits and underscores")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dbExists(in.Name) {
		return conflict("database %s already exists", in.Name)
	}
	// No IF NOT EXISTS: refuse to adopt a database the panel did not create.
	if _, err := a.sql("CREATE DATABASE `" + in.Name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		return bad("%v", err)
	}
	if _, err := a.DB.Exec(`INSERT INTO dbs(name, created) VALUES(?,?)`, in.Name, time.Now().Unix()); err != nil {
		return err
	}
	a.audit("database.created", in.Name)
	writeJSON(w, 201, map[string]bool{"ok": true})
	return nil
}

func (a *App) deleteDatabase(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	a.mu.Lock()
	defer a.mu.Unlock()
	if !reDBName.MatchString(name) || !a.dbExists(name) {
		return notFound("database")
	}
	stmts := []string{}
	rows, err := a.DB.Query(`SELECT user FROM db_grants WHERE db=?`, name)
	if err != nil {
		return err
	}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return err
		}
		stmts = append(stmts, "REVOKE ALL PRIVILEGES ON "+grantPattern(name)+".* FROM "+sqlString(u)+"@'localhost'")
	}
	rows.Close()
	stmts = append(stmts, "DROP DATABASE `"+name+"`")
	if _, err := a.sql(stmts...); err != nil {
		return bad("%v", err)
	}
	_, _ = a.DB.Exec(`DELETE FROM db_grants WHERE db=?`, name)
	if _, err := a.DB.Exec(`DELETE FROM dbs WHERE name=?`, name); err != nil {
		return err
	}
	a.audit("database.deleted", name)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

type dbUserInput struct {
	Name      string   `json:"name"`
	Password  string   `json:"password"`
	Databases []string `json:"databases"`
}

func (a *App) checkDBList(dbs []string) error {
	for _, d := range dbs {
		if !reDBName.MatchString(d) || !a.dbExists(d) {
			return bad("unknown database %q", d)
		}
	}
	return nil
}

func (a *App) createDBUser(w http.ResponseWriter, r *http.Request) error {
	var in dbUserInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !reDBUser.MatchString(in.Name) || systemDBUsers[strings.ToLower(in.Name)] {
		return bad("user name may only contain letters, digits and underscores (max 32)")
	}
	if err := validPassword(in.Password); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.checkDBList(in.Databases); err != nil {
		return err
	}
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM db_users WHERE name=?`, in.Name).Scan(&n)
	if n > 0 {
		return conflict("user %s already exists", in.Name)
	}
	who := sqlString(in.Name) + "@'localhost'"
	stmts := []string{"CREATE USER " + who + " IDENTIFIED BY " + sqlString(in.Password)}
	for _, d := range in.Databases {
		stmts = append(stmts, "GRANT ALL PRIVILEGES ON "+grantPattern(d)+".* TO "+who)
	}
	if _, err := a.sql(stmts...); err != nil {
		return bad("%v", err)
	}
	if _, err := a.DB.Exec(`INSERT INTO db_users(name, created) VALUES(?,?)`, in.Name, time.Now().Unix()); err != nil {
		return err
	}
	for _, d := range in.Databases {
		_, _ = a.DB.Exec(`INSERT OR IGNORE INTO db_grants(user, db) VALUES(?,?)`, in.Name, d)
	}
	a.audit("dbuser.created", in.Name)
	writeJSON(w, 201, map[string]bool{"ok": true})
	return nil
}

func (a *App) updateDBUser(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	var in dbUserInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM db_users WHERE name=?`, name).Scan(&n)
	if n == 0 || !reDBUser.MatchString(name) {
		return notFound("database user")
	}
	if in.Password != "" {
		if err := validPassword(in.Password); err != nil {
			return err
		}
	}
	if err := a.checkDBList(in.Databases); err != nil {
		return err
	}
	current := map[string]bool{}
	rows, err := a.DB.Query(`SELECT db FROM db_grants WHERE user=?`, name)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return err
		}
		current[d] = true
	}
	rows.Close()

	who := sqlString(name) + "@'localhost'"
	stmts := []string{}
	if in.Password != "" {
		stmts = append(stmts, "ALTER USER "+who+" IDENTIFIED BY "+sqlString(in.Password))
	}
	want := map[string]bool{}
	for _, d := range in.Databases {
		want[d] = true
		if !current[d] {
			stmts = append(stmts, "GRANT ALL PRIVILEGES ON "+grantPattern(d)+".* TO "+who)
		}
	}
	for d := range current {
		if !want[d] {
			stmts = append(stmts, "REVOKE ALL PRIVILEGES ON "+grantPattern(d)+".* FROM "+who)
		}
	}
	if len(stmts) > 0 {
		if _, err := a.sql(stmts...); err != nil {
			return bad("%v", err)
		}
	}
	_, _ = a.DB.Exec(`DELETE FROM db_grants WHERE user=?`, name)
	for d := range want {
		_, _ = a.DB.Exec(`INSERT OR IGNORE INTO db_grants(user, db) VALUES(?,?)`, name, d)
	}
	a.audit("dbuser.updated", name)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) deleteDBUser(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	a.mu.Lock()
	defer a.mu.Unlock()
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM db_users WHERE name=?`, name).Scan(&n)
	if n == 0 || !reDBUser.MatchString(name) {
		return notFound("database user")
	}
	if _, err := a.sql("DROP USER " + sqlString(name) + "@'localhost'"); err != nil {
		return bad("%v", err)
	}
	_, _ = a.DB.Exec(`DELETE FROM db_grants WHERE user=?`, name)
	if _, err := a.DB.Exec(`DELETE FROM db_users WHERE name=?`, name); err != nil {
		return err
	}
	a.audit("dbuser.deleted", name)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
