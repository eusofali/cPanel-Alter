package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hostdeck/internal/sys"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newTestApp(t *testing.T) (*App, *client) {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &App{DB: db, Log: log, Sys: &sys.Sys{Dev: true, Root: filepath.Join(dir, "root"), Log: log}}
	if err := a.SetAdminPassword("correct horse"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	c := &client{t, srv.URL, &http.Client{Jar: jar}}
	return a, c
}

func (c *client) do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, c.base+path, &buf)
	req.Header.Set("X-Requested-With", "hostdeck")
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func (c *client) must(method, path string, body any, want int) map[string]any {
	c.t.Helper()
	status, out := c.do(method, path, body)
	if status != want {
		c.t.Fatalf("%s %s: status %d, want %d (%v)", method, path, status, want, out)
	}
	return out
}

func (c *client) login() {
	c.must("POST", "/api/login", map[string]string{"password": "correct horse"}, 200)
}

func TestAuthRequired(t *testing.T) {
	_, c := newTestApp(t)
	c.must("GET", "/api/sites", nil, 401)
	c.must("POST", "/api/login", map[string]string{"password": "nope"}, 401)
	c.login()
	c.must("GET", "/api/sites", nil, 200)

	// State-changing requests without the custom header are rejected (CSRF).
	req, _ := http.NewRequest("POST", c.base+"/api/logout", nil)
	res, _ := c.http.Do(req)
	if res.StatusCode != 403 {
		t.Fatalf("missing header: status %d, want 403", res.StatusCode)
	}
	c.must("POST", "/api/logout", nil, 200)
	c.must("GET", "/api/sites", nil, 401)
}

func TestLoginThrottle(t *testing.T) {
	_, c := newTestApp(t)
	for i := 0; i < maxFailures; i++ {
		c.must("POST", "/api/login", map[string]string{"password": "bad"}, 401)
	}
	c.must("POST", "/api/login", map[string]string{"password": "correct horse"}, 429)
}

func TestSiteLifecycle(t *testing.T) {
	a, c := newTestApp(t)
	c.login()
	site := c.must("POST", "/api/sites", map[string]any{"domain": "Example.com", "aliases": []string{"www.example.com"}, "type": "php", "php_version": "8.3"}, 201)
	id := int64(site["id"].(float64))

	conf, err := a.Sys.ReadFile(nginxDir + "/example.com.conf")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"server_name example.com www.example.com;", "fastcgi_pass unix:/run/php/hostdeck-site1.sock;", "root /var/www/example.com/public_html;"} {
		if !strings.Contains(string(conf), want) {
			t.Errorf("vhost missing %q\n%s", want, conf)
		}
	}
	if strings.Contains(string(conf), "listen 443") {
		t.Error("vhost has TLS before a certificate was issued")
	}
	pool, err := a.Sys.ReadFile(poolFile("8.3", id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pool), "pm = ondemand") || !strings.Contains(string(pool), "open_basedir] = /var/www/example.com/:") {
		t.Errorf("unexpected pool:\n%s", pool)
	}
	if !a.Sys.Exists("/var/www/example.com/public_html/index.html") {
		t.Error("placeholder page missing")
	}

	// Duplicates and bad input.
	c.must("POST", "/api/sites", map[string]any{"domain": "example.com", "type": "static"}, 409)
	c.must("POST", "/api/sites", map[string]any{"domain": "other.com", "aliases": []string{"www.example.com"}, "type": "static"}, 409)
	c.must("POST", "/api/sites", map[string]any{"domain": "bad domain; rm -rf /", "type": "static"}, 400)
	c.must("POST", "/api/sites", map[string]any{"domain": "p.com", "type": "proxy", "proxy_target": "http://127.0.0.1:3000; evil"}, 400)
	c.must("POST", "/api/sites", map[string]any{"domain": "p.com", "type": "php", "php_version": "5.6"}, 400)

	// SSL needs an admin email first.
	c.must("POST", "/api/sites/1/ssl", nil, 400)
	c.must("PUT", "/api/settings", map[string]string{"admin_email": "admin@example.com", "server_ip": "203.0.113.5"}, 200)
	c.must("POST", "/api/sites/1/ssl", nil, 200)
	conf, _ = a.Sys.ReadFile(nginxDir + "/example.com.conf")
	if !strings.Contains(string(conf), "listen 443 ssl") || !strings.Contains(string(conf), "return 301 https://") {
		t.Errorf("TLS vhost wrong:\n%s", conf)
	}

	// Switching PHP version moves the pool; switching to static removes it.
	c.must("PUT", "/api/sites/1", map[string]any{"type": "php", "php_version": "8.2", "aliases": []string{}}, 200)
	if a.Sys.Exists(poolFile("8.3", id)) || !a.Sys.Exists(poolFile("8.2", id)) {
		t.Error("pool did not move to PHP 8.2")
	}
	c.must("PUT", "/api/sites/1", map[string]any{"type": "static"}, 200)
	if a.Sys.Exists(poolFile("8.2", id)) {
		t.Error("pool not removed for static site")
	}

	c.must("DELETE", "/api/sites/1?files=1", nil, 200)
	if a.Sys.Exists(nginxDir+"/example.com.conf") || a.Sys.Exists("/var/www/example.com") {
		t.Error("site not fully removed")
	}
}

func TestApps(t *testing.T) {
	a, c := newTestApp(t)
	c.login()
	c.must("POST", "/api/sites", map[string]any{"domain": "node.example.com", "type": "node"}, 201)
	c.must("POST", "/api/sites", map[string]any{"domain": "py.example.com", "type": "python", "entry": "shop.wsgi:application"}, 201)
	c.must("POST", "/api/sites", map[string]any{"domain": "bad.example.com", "type": "node", "entry": "../../etc/x.js"}, 400)
	c.must("POST", "/api/sites", map[string]any{"domain": "bad.example.com", "type": "python", "entry": "app:app; rm -rf /"}, 400)

	unit, err := a.Sys.ReadFile(unitFile(1))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"User=www-data", "ExecStart=/usr/bin/node app.js", "PORT=20001", "ReadWritePaths=/var/www/node.example.com", "WorkingDirectory=/var/www/node.example.com/app"} {
		if !strings.Contains(string(unit), want) {
			t.Errorf("node unit missing %q\n%s", want, unit)
		}
	}
	unit, _ = a.Sys.ReadFile(unitFile(2))
	if !strings.Contains(string(unit), "ExecStart=/var/www/py.example.com/venv/bin/gunicorn --workers 2 --bind 127.0.0.1:20002 shop.wsgi:application") {
		t.Errorf("python unit wrong:\n%s", unit)
	}
	if !a.Sys.Exists("/var/www/node.example.com/app/app.js") || !a.Sys.Exists("/var/www/py.example.com/app/shop/wsgi.py") {
		t.Error("starter files missing")
	}
	conf, _ := a.Sys.ReadFile(nginxDir + "/node.example.com.conf")
	if !strings.Contains(string(conf), "proxy_pass http://127.0.0.1:20001;") {
		t.Errorf("vhost does not proxy to the app:\n%s", conf)
	}

	c.must("POST", "/api/sites/1/app/install", nil, 400) // no package.json yet
	c.must("PUT", "/api/files/content", map[string]string{"path": "/node.example.com/app/package.json", "content": "{}"}, 200)
	c.must("POST", "/api/sites/1/app/install", nil, 200)
	c.must("POST", "/api/sites/1/app/restart", nil, 200)
	c.must("GET", "/api/sites/1/app/logs", nil, 200)

	// Disabling or changing type removes the service.
	c.must("PUT", "/api/sites/1", map[string]any{"type": "static"}, 200)
	if a.Sys.Exists(unitFile(1)) {
		t.Error("unit not removed after switching to static")
	}
	c.must("POST", "/api/sites/1/app/restart", nil, 400)
	c.must("DELETE", "/api/sites/2", nil, 200)
	if a.Sys.Exists(unitFile(2)) {
		t.Error("unit not removed with the site")
	}
}

func TestPHPExtensions(t *testing.T) {
	a, c := newTestApp(t)
	c.login()
	out := c.must("GET", "/api/php", nil, 200)
	kinds := map[string]string{}
	installed := map[string]bool{}
	for _, e := range out["versions"].([]any)[0].(map[string]any)["extensions"].([]any) {
		m := e.(map[string]any)
		kinds[m["name"].(string)] = m["kind"].(string)
		installed[m["name"].(string)] = m["installed"].(bool)
	}
	if kinds["sodium"] != "builtin" || !installed["sodium"] {
		t.Errorf("sodium should be listed as built in and enabled: %v %v", kinds["sodium"], installed["sodium"])
	}
	if kinds["sourceguardian"] != "vendor" || installed["sourceguardian"] {
		t.Errorf("sourceguardian should be offered, not installed")
	}
	c.must("POST", "/api/php/8.3/extensions", map[string]any{"name": "sodium", "install": false}, 400)
	c.must("POST", "/api/php/8.3/extensions", map[string]any{"name": "sourceguardian", "install": true}, 200)
	ini, err := a.Sys.ReadFile(sgIni("8.3"))
	if err != nil || !strings.Contains(string(ini), "extension=ixed.8.3.lin") {
		t.Errorf("sourceguardian ini wrong: %q %v", ini, err)
	}
	c.must("POST", "/api/php/8.3/extensions", map[string]any{"name": "sourceguardian", "install": false}, 200)
	if a.Sys.Exists(sgIni("8.3")) {
		t.Error("sourceguardian ini not removed")
	}
	c.must("POST", "/api/php/8.3/extensions", map[string]any{"name": "nonexistent", "install": true}, 400)
	c.must("POST", "/api/php/8.3/extensions", map[string]any{"name": "redis; reboot", "install": true}, 400)
}

func TestFileJail(t *testing.T) {
	a, c := newTestApp(t)
	c.login()
	root := a.Sys.Path(WebRoot)
	_ = os.MkdirAll(filepath.Join(root, "site"), 0o755)
	secret := filepath.Join(a.Sys.Root, "secret.txt")
	_ = os.WriteFile(secret, []byte("top secret"), 0o600)
	_ = os.Symlink(secret, filepath.Join(root, "site", "link"))
	_ = os.Symlink(a.Sys.Root, filepath.Join(root, "site", "dirlink"))

	c.must("PUT", "/api/files/content", map[string]string{"path": "/site/a.txt", "content": "hello"}, 200)
	got := c.must("GET", "/api/files/content?path=/site/a.txt", nil, 200)
	if got["content"] != "hello" {
		t.Fatalf("content = %v", got["content"])
	}
	// ".." is neutralised: this resolves inside the root, where nothing exists.
	c.must("GET", "/api/files/content?path=/../../secret.txt", nil, 404)
	c.must("GET", "/api/files/content?path=/site/link", nil, 403)
	c.must("GET", "/api/files/download?path=/site/dirlink/secret.txt", nil, 403)
	c.must("PUT", "/api/files/content", map[string]string{"path": "/site/dirlink/new.txt", "content": "x"}, 403)
	c.must("DELETE", "/api/files?path=/", nil, 400)
	c.must("POST", "/api/files/mkdir", map[string]string{"path": "/site", "name": "../up"}, 400)
	c.must("POST", "/api/files/rename", map[string]string{"path": "/site/a.txt", "name": "b.txt"}, 200)
	c.must("DELETE", "/api/files?path=/site/b.txt", nil, 200)
	if b, _ := os.ReadFile(secret); string(b) != "top secret" {
		t.Fatal("file outside the root was modified")
	}
}

func TestDatabasesFTPMail(t *testing.T) {
	a, c := newTestApp(t)
	c.login()
	c.must("POST", "/api/databases", map[string]string{"name": "shop_db"}, 201)
	c.must("POST", "/api/databases", map[string]string{"name": "shop_db"}, 409)
	c.must("POST", "/api/databases", map[string]string{"name": "x`; DROP DATABASE mysql"}, 400)
	c.must("POST", "/api/databases", map[string]string{"name": "mysql"}, 400)
	c.must("POST", "/api/dbusers", map[string]any{"name": "shop", "password": "pa'ss\\word1", "databases": []string{"shop_db"}}, 201)
	c.must("POST", "/api/dbusers", map[string]any{"name": "root", "password": "password1"}, 400)
	c.must("POST", "/api/dbusers", map[string]any{"name": "u2", "password": "password1", "databases": []string{"nope"}}, 400)
	c.must("DELETE", "/api/databases/shop_db", nil, 200)

	_ = a.Sys.MkdirAll("/var/www/example.com", 0o755)
	c.must("POST", "/api/ftp", map[string]string{"username": "deploy", "password": "password1", "home": "/example.com"}, 201)
	c.must("POST", "/api/ftp", map[string]string{"username": "all", "password": "password1", "home": "/"}, 400)
	c.must("POST", "/api/ftp", map[string]string{"username": "esc", "password": "password1", "home": "/../../etc"}, 400)
	passwd, _ := a.Sys.ReadFile(ftpPasswd)
	if !strings.HasPrefix(string(passwd), "deploy:$2a$") || !strings.Contains(string(passwd), ":/var/www/example.com/./:") {
		t.Errorf("ftp passwd wrong: %s", passwd)
	}

	c.must("POST", "/api/mail/domains", map[string]string{"domain": "example.com"}, 201)
	c.must("POST", "/api/mail/mailboxes", map[string]any{"domain_id": 1, "local": "info", "password": "password1", "quota_mb": 500}, 201)
	c.must("POST", "/api/mail/mailboxes", map[string]any{"domain_id": 1, "local": "in fo", "password": "password1"}, 400)
	c.must("POST", "/api/mail/aliases", map[string]any{"domain_id": 1, "source": "info", "destination": "a@b.com"}, 409)
	c.must("POST", "/api/mail/aliases", map[string]any{"domain_id": 1, "source": "*", "destination": "info@example.com"}, 201)
	c.must("POST", "/api/mail/aliases", map[string]any{"domain_id": 1, "source": "x", "destination": "a@b.com\nroot@evil"}, 400)
	users, _ := a.Sys.ReadFile(dovecotUsers)
	if !strings.HasPrefix(string(users), "info@example.com:{BLF-CRYPT}$2a$") || !strings.Contains(string(users), "storage=500M") {
		t.Errorf("dovecot users wrong: %s", users)
	}
	valias, _ := a.Sys.ReadFile(postfixDir + "/valias")
	if string(valias) != "@example.com info@example.com\n" {
		t.Errorf("valias = %q", valias)
	}
}

func TestDNS(t *testing.T) {
	a, c := newTestApp(t)
	c.login()
	c.must("POST", "/api/dns/zones", map[string]any{"domain": "example.com", "ip": "203.0.113.5", "mail": true}, 201)
	c.must("POST", "/api/dns/zones/1/records", map[string]any{"name": "blog.example.com.", "type": "cname", "value": "example.com"}, 201)
	c.must("POST", "/api/dns/zones/1/records", map[string]any{"name": "blog", "type": "A", "value": "203.0.113.9"}, 409)
	c.must("POST", "/api/dns/zones/1/records", map[string]any{"name": "@", "type": "CNAME", "value": "x.example.org"}, 400)
	c.must("POST", "/api/dns/zones/1/records", map[string]any{"name": "a", "type": "A", "value": "999.1.1.1"}, 400)
	c.must("POST", "/api/dns/zones/1/records", map[string]any{"name": "a", "type": "TXT", "value": "line\nbreak"}, 400)
	c.must("POST", "/api/dns/zones/1/records", map[string]any{"name": "_dmarc", "type": "TXT", "value": `v=DMARC1; p="none"`}, 201)
	c.must("POST", "/api/dns/zones/1/records", map[string]any{"name": "_sip._tcp", "type": "SRV", "prio": 10, "value": "5 5060 sip.example.com"}, 201)

	zone, err := a.Sys.ReadFile(bindDir + "/db.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"$ORIGIN example.com.", "@ IN SOA ns1.example.com. hostmaster.example.com.",
		"@ 3600 IN MX 10 mail.example.com.", "blog 3600 IN CNAME example.com.",
		`_dmarc 3600 IN TXT "v=DMARC1; p=\"none\""`, "_sip._tcp 3600 IN SRV 10 5 5060 sip.example.com.",
	} {
		if !strings.Contains(string(zone), want) {
			t.Errorf("zone missing %q\n%s", want, zone)
		}
	}
	conf, _ := a.Sys.ReadFile(bindConf)
	if !strings.Contains(string(conf), `zone "example.com"`) {
		t.Errorf("zones.conf = %s", conf)
	}
}

func TestHelpers(t *testing.T) {
	now := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	if got := nextSerial(0, now); got != 2026030400 {
		t.Errorf("nextSerial fresh = %d", got)
	}
	if got := nextSerial(2026030400, now); got != 2026030401 {
		t.Errorf("nextSerial same day = %d", got)
	}
	if got := sqlString(`a'b\c`); got != `'a\'b\\c'` {
		t.Errorf("sqlString = %s", got)
	}
	if got := grantPattern("shop_1"); got != "`shop\\_1`" {
		t.Errorf("grantPattern = %s", got)
	}
	if long := txtChunks(strings.Repeat("a", 300)); strings.Count(long, `"`) != 4 {
		t.Errorf("txtChunks did not split: %s", long)
	}
	for _, d := range []string{"example.com", "a.b-c.io", "xn--bcher-kva.example"} {
		if !validDomain(d) {
			t.Errorf("validDomain(%q) = false", d)
		}
	}
	for _, d := range []string{"", "localhost", "-a.com", "a..com", "a.com;", "a b.com", "exa_mple.com"} {
		if validDomain(d) {
			t.Errorf("validDomain(%q) = true", d)
		}
	}
}
