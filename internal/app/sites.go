package app

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"
)

const (
	nginxDir   = "/etc/nginx/hmea-panel/sites"
	acmeRoot   = "/var/www/_letsencrypt"
	leLiveRoot = "/etc/letsencrypt/live"
)

type Site struct {
	ID         int64    `json:"id"`
	Domain     string   `json:"domain"`
	Aliases    []string `json:"aliases"`
	Type       string   `json:"type"`  // php | static | proxy | node | python
	Entry      string   `json:"entry"` // node: script path, python: WSGI "module:variable"
	Port       int      `json:"port"`  // local port of a node/python app
	PHP        string   `json:"php_version"`
	Proxy      string   `json:"proxy_target"`
	SSL        bool     `json:"ssl"`
	ForceHTTPS bool     `json:"force_https"`
	Enabled    bool     `json:"enabled"`
	Docroot    string   `json:"docroot"`
	Created    int64    `json:"created"`
}

func siteDir(domain string) string { return WebRoot + "/" + domain }
func docroot(domain string) string { return siteDir(domain) + "/public_html" }

var vhostTmpl = template.Must(template.New("vhost").Parse(`# Managed by hmea-panel. Changes made here are overwritten.
{{define "body"}}    server_name {{.Names}};
    access_log /var/log/nginx/{{.Domain}}.access.log;
    error_log  /var/log/nginx/{{.Domain}}.error.log;
    client_max_body_size 128m;
    server_tokens off;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;

    location ^~ /.well-known/acme-challenge/ {
        root {{.ACME}};
        default_type text/plain;
    }
{{if .Proxied}}
    location / {
        proxy_pass {{.Proxy}};
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
{{else}}
    root {{.Docroot}};
    index {{if eq .Type "php"}}index.php {{end}}index.html;

    location / {
        try_files $uri $uri/ {{if eq .Type "php"}}/index.php?$args{{else}}=404{{end}};
    }
{{if eq .Type "php"}}
    location ~ \.php$ {
        include snippets/fastcgi-php.conf;
        fastcgi_pass unix:{{.Sock}};
    }
{{end}}
    location ~ /\.(?!well-known) {
        deny all;
    }
    location ~* /(uploads|files|cache)/.*\.php$ {
        deny all;
    }
{{end}}{{end}}server {
    listen 80;
{{if .IPv6}}    listen [::]:80;
{{end}}{{if and .SSL .ForceHTTPS}}    server_name {{.Names}};

    location ^~ /.well-known/acme-challenge/ {
        root {{.ACME}};
        default_type text/plain;
    }
    location / {
        return 301 https://$host$request_uri;
    }
{{else}}{{template "body" .}}{{end}}}
{{if .SSL}}
server {
    listen 443 ssl http2;
{{if .IPv6}}    listen [::]:443 ssl http2;
{{end}}    ssl_certificate     {{.Live}}/{{.Domain}}/fullchain.pem;
    ssl_certificate_key {{.Live}}/{{.Domain}}/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
{{template "body" .}}}
{{end}}`))

// hasIPv6 reports whether the kernel has IPv6 at all; Nginx refuses to
// start with an IPv6 listener on servers where it is switched off.
var hasIPv6 = func() bool {
	_, err := os.Stat("/proc/net/if_inet6")
	return err == nil
}

func renderVhost(s Site) (string, error) {
	var buf bytes.Buffer
	proxy := s.Proxy
	if s.isApp() {
		proxy = fmt.Sprintf("http://127.0.0.1:%d", s.Port)
	}
	err := vhostTmpl.Execute(&buf, map[string]any{
		"Proxied": s.Type == "proxy" || s.isApp(), "IPv6": hasIPv6(),
		"Domain": s.Domain, "Names": strings.Join(append([]string{s.Domain}, s.Aliases...), " "),
		"Type": s.Type, "PHP": s.PHP, "Proxy": proxy, "SSL": s.SSL, "ForceHTTPS": s.ForceHTTPS,
		"Sock": poolSock(s.ID), "Docroot": docroot(s.Domain), "ACME": acmeRoot, "Live": leLiveRoot,
	})
	return buf.String(), err
}

func poolSock(id int64) string { return fmt.Sprintf("/run/php/hmea-site%d.sock", id) }
func poolFile(version string, id int64) string {
	return fmt.Sprintf("/etc/php/%s/fpm/pool.d/hmea-site%d.conf", version, id)
}

// Each PHP site gets its own FPM pool. "ondemand" keeps idle sites at zero
// workers (important on small VPSes) and open_basedir stops one site's code
// from reading another site's files.
var poolTmpl = template.Must(template.New("pool").Parse(`; Managed by hmea-panel. Changes made here are overwritten.
[site{{.ID}}]
user = www-data
group = www-data
listen = {{.Sock}}
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
pm = ondemand
pm.max_children = 5
pm.process_idle_timeout = 20s
pm.max_requests = 500
php_admin_value[open_basedir] = {{.Dir}}/:/usr/share/php/
php_admin_value[upload_tmp_dir] = {{.Dir}}/tmp
php_admin_value[sys_temp_dir] = {{.Dir}}/tmp
php_admin_value[session.save_path] = {{.Dir}}/tmp
php_admin_value[disable_functions] = exec,passthru,shell_exec,system,proc_open,popen,pcntl_exec
php_admin_flag[allow_url_include] = off
php_admin_flag[expose_php] = off
php_value[upload_max_filesize] = 128M
php_value[post_max_size] = 128M
php_value[memory_limit] = 256M
`))

func renderPool(s Site) (string, error) {
	var buf bytes.Buffer
	err := poolTmpl.Execute(&buf, map[string]any{"ID": s.ID, "Sock": poolSock(s.ID), "Dir": siteDir(s.Domain)})
	return buf.String(), err
}

// applyPool makes the FPM pool files match the site and reloads the PHP
// versions whose pool set changed.
func (a *App) applyPool(s Site) error {
	want := ""
	if s.Enabled && s.Type == "php" {
		want = s.PHP
	}
	for _, v := range a.phpVersions() {
		path := poolFile(v, s.ID)
		previous, prevErr := a.Sys.ReadFile(path)
		if v == want {
			conf, err := renderPool(s)
			if err != nil {
				return err
			}
			if prevErr == nil && string(previous) == conf {
				continue
			}
			if err := a.Sys.WriteFile(path, []byte(conf), 0o644); err != nil {
				return err
			}
		} else {
			if prevErr != nil {
				continue
			}
			if err := a.Sys.Remove(path); err != nil {
				return err
			}
		}
		if _, err := a.Sys.Run("", "php-fpm"+v, "-t"); err != nil {
			if prevErr == nil {
				_ = a.Sys.WriteFile(path, previous, 0o644)
			} else {
				_ = a.Sys.Remove(path)
			}
			return bad("php-fpm %s rejected the pool configuration: %v", v, err)
		}
		if _, err := a.Sys.Run("", "systemctl", "reload", "php"+v+"-fpm"); err != nil {
			return bad("%v", err)
		}
	}
	return nil
}

func (a *App) loadSites() ([]Site, error) {
	rows, err := a.DB.Query(`SELECT id, domain, aliases, type, php, proxy, ssl, force_https, enabled, created, entry, port FROM sites ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Site{}
	for rows.Next() {
		var s Site
		var aliases string
		if err := rows.Scan(&s.ID, &s.Domain, &aliases, &s.Type, &s.PHP, &s.Proxy, &s.SSL, &s.ForceHTTPS, &s.Enabled, &s.Created, &s.Entry, &s.Port); err != nil {
			return nil, err
		}
		s.Aliases = strings.Fields(aliases)
		s.Docroot = docroot(s.Domain)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (a *App) loadSite(id int64) (Site, error) {
	sites, err := a.loadSites()
	if err != nil {
		return Site{}, err
	}
	for _, s := range sites {
		if s.ID == id {
			return s, nil
		}
	}
	return Site{}, notFound("site")
}

// applySite writes (or removes) the vhost, validates the whole nginx config
// and rolls the file back if nginx rejects it.
func (a *App) applySite(s Site) error {
	if err := a.applyPool(s); err != nil {
		return err
	}
	if err := a.applyApp(s); err != nil {
		return err
	}
	path := nginxDir + "/" + s.Domain + ".conf"
	previous, prevErr := a.Sys.ReadFile(path)
	if s.Enabled {
		conf, err := renderVhost(s)
		if err != nil {
			return err
		}
		if err := a.Sys.WriteFile(path, []byte(conf), 0o644); err != nil {
			return err
		}
	} else if err := a.Sys.Remove(path); err != nil {
		return err
	}
	if _, err := a.Sys.Run("", "nginx", "-t"); err != nil {
		if prevErr == nil {
			_ = a.Sys.WriteFile(path, previous, 0o644)
		} else {
			_ = a.Sys.Remove(path)
		}
		return bad("nginx rejected the configuration: %v", err)
	}
	if _, err := a.Sys.Run("", "systemctl", "reload", "nginx"); err != nil {
		return bad("%v", err)
	}
	return nil
}

type siteInput struct {
	Domain     string   `json:"domain"`
	Aliases    []string `json:"aliases"`
	Type       string   `json:"type"`
	PHP        string   `json:"php_version"`
	Proxy      string   `json:"proxy_target"`
	Entry      string   `json:"entry"`
	ForceHTTPS bool     `json:"force_https"`
	Enabled    *bool    `json:"enabled"`
}

// validateSite normalises the input in place. selfID is the site being
// edited (0 when creating) so it does not conflict with itself.
func (a *App) validateSite(in *siteInput, domain string, selfID int64) error {
	seen := map[string]bool{domain: true}
	aliases := []string{}
	for _, raw := range in.Aliases {
		al := normDomain(raw)
		if al == "" || seen[al] {
			continue
		}
		if !validDomain(al) {
			return bad("invalid alias %q", raw)
		}
		seen[al] = true
		aliases = append(aliases, al)
	}
	in.Aliases = aliases

	sites, err := a.loadSites()
	if err != nil {
		return err
	}
	for _, other := range sites {
		if other.ID == selfID {
			continue
		}
		for _, name := range append([]string{other.Domain}, other.Aliases...) {
			if seen[name] {
				return conflict("%s is already used by site %s", name, other.Domain)
			}
		}
	}

	switch in.Type {
	case "php":
		ok := false
		for _, v := range a.phpVersions() {
			ok = ok || v == in.PHP
		}
		if !ok {
			return bad("PHP version %q is not installed", in.PHP)
		}
		in.Proxy = ""
	case "static":
		in.PHP, in.Proxy = "", ""
	case "proxy":
		in.Proxy = strings.TrimSuffix(strings.TrimSpace(in.Proxy), "/")
		if !reProxy.MatchString(in.Proxy) {
			return bad("proxy target must look like http://127.0.0.1:3000")
		}
		in.PHP = ""
	case "node":
		in.Entry = strings.TrimSpace(in.Entry)
		if in.Entry == "" {
			in.Entry = "app.js"
		}
		if !reNodeEntry.MatchString(in.Entry) || strings.Contains(in.Entry, "..") {
			return bad("start file must be a .js, .mjs or .cjs path inside the app folder, such as app.js")
		}
		if a.runtimes()["node"] == "" {
			return bad("Node.js is not installed on this server")
		}
		in.PHP, in.Proxy = "", ""
	case "python":
		in.Entry = strings.TrimSpace(in.Entry)
		if in.Entry == "" {
			in.Entry = "app:app"
		}
		if !rePyEntry.MatchString(in.Entry) {
			return bad("WSGI entry must look like module:variable, such as app:app")
		}
		if a.runtimes()["python"] == "" {
			return bad("Python 3 is not installed on this server")
		}
		in.PHP, in.Proxy = "", ""
	default:
		return bad("type must be php, static, proxy, node or python")
	}
	if in.Type != "node" && in.Type != "python" {
		in.Entry = ""
	}
	return nil
}

const placeholderPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>%DOMAIN%</title>
<style>body{font-family:system-ui,sans-serif;display:grid;place-items:center;min-height:100vh;margin:0;background:#f6f7f9;color:#1c2330}</style>
</head><body><div><h1>%DOMAIN%</h1><p>This site is ready. Upload your files to public_html.</p></div></body></html>
`

func (a *App) listSites(w http.ResponseWriter, r *http.Request) error {
	sites, err := a.loadSites()
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"sites": sites, "php_versions": a.phpVersions(), "runtimes": a.runtimes()})
	return nil
}

func (a *App) createSite(w http.ResponseWriter, r *http.Request) error {
	var in siteInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	domain := normDomain(in.Domain)
	if !validDomain(domain) {
		return bad("invalid domain name")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.validateSite(&in, domain, 0); err != nil {
		return err
	}
	s := Site{Domain: domain, Aliases: in.Aliases, Type: in.Type, PHP: in.PHP, Proxy: in.Proxy, Entry: in.Entry, Enabled: true, Created: time.Now().Unix()}

	if err := a.Sys.MkdirAll(docroot(domain), 0o755); err != nil {
		return err
	}
	if err := a.Sys.MkdirAll(siteDir(domain)+"/tmp", 0o750); err != nil {
		return err
	}
	if err := a.Sys.MkdirAll(acmeRoot, 0o755); err != nil {
		return err
	}
	index := docroot(domain) + "/index.html"
	if (s.Type == "php" || s.Type == "static") && !a.Sys.Exists(index) && !a.Sys.Exists(docroot(domain)+"/index.php") {
		if err := a.Sys.WriteFile(index, []byte(strings.ReplaceAll(placeholderPage, "%DOMAIN%", domain)), 0o644); err != nil {
			return err
		}
	}
	if _, err := a.Sys.Run("", "chown", "-R", "www-data:www-data", siteDir(domain)); err != nil {
		return bad("%v", err)
	}

	res, err := a.DB.Exec(`INSERT INTO sites(domain, aliases, type, php, proxy, entry, created) VALUES(?,?,?,?,?,?,?)`,
		s.Domain, strings.Join(s.Aliases, " "), s.Type, s.PHP, s.Proxy, s.Entry, s.Created)
	if isUnique(err) {
		return conflict("site %s already exists", domain)
	} else if err != nil {
		return err
	}
	s.ID, _ = res.LastInsertId()
	s.Port = appPortBase + int(s.ID)
	_, _ = a.DB.Exec(`UPDATE sites SET port=? WHERE id=?`, s.Port, s.ID)
	err = a.prepareApp(s)
	if err == nil {
		err = a.applySite(s)
	}
	if err != nil {
		s.Enabled = false
		_ = a.applyApp(s)
		_, _ = a.DB.Exec(`DELETE FROM sites WHERE id=?`, s.ID)
		return err
	}
	a.audit("site.created", domain)
	s.Docroot = docroot(domain)
	writeJSON(w, 201, s)
	return nil
}

func (a *App) updateSite(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in siteInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old, err := a.loadSite(id)
	if err != nil {
		return err
	}
	if err := a.validateSite(&in, old.Domain, id); err != nil {
		return err
	}
	s := old
	s.Aliases, s.Type, s.PHP, s.Proxy, s.Entry, s.ForceHTTPS = in.Aliases, in.Type, in.PHP, in.Proxy, in.Entry, in.ForceHTTPS
	if in.Enabled != nil {
		s.Enabled = *in.Enabled
	}
	if s.Port == 0 {
		s.Port = appPortBase + int(s.ID)
	}
	if err := a.prepareApp(s); err != nil {
		return err
	}
	if err := a.applySite(s); err != nil {
		return err
	}
	if _, err := a.DB.Exec(`UPDATE sites SET aliases=?, type=?, php=?, proxy=?, entry=?, port=?, force_https=?, enabled=? WHERE id=?`,
		strings.Join(s.Aliases, " "), s.Type, s.PHP, s.Proxy, s.Entry, s.Port, s.ForceHTTPS, s.Enabled, id); err != nil {
		return err
	}
	a.audit("site.updated", s.Domain)
	writeJSON(w, 200, s)
	return nil
}

func (a *App) deleteSite(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, err := a.loadSite(id)
	if err != nil {
		return err
	}
	s.Enabled = false
	if err := a.applySite(s); err != nil {
		return err
	}
	if _, err := a.DB.Exec(`DELETE FROM sites WHERE id=?`, id); err != nil {
		return err
	}
	detail := s.Domain
	if r.URL.Query().Get("files") == "1" {
		if err := a.Sys.RemoveAll(siteDir(s.Domain)); err != nil {
			return err
		}
		detail += " (files removed)"
	}
	a.audit("site.deleted", detail)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) issueSSL(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, err := a.loadSite(id)
	if err != nil {
		return err
	}
	if !s.Enabled {
		return bad("enable the site before requesting a certificate")
	}
	email := a.setting("admin_email")
	if email == "" {
		return bad("set an admin email in Settings first; Let's Encrypt needs it")
	}
	args := []string{"certonly", "--webroot", "-w", acmeRoot, "--non-interactive", "--agree-tos",
		"-m", email, "--cert-name", s.Domain, "--expand", "-d", s.Domain}
	for _, al := range s.Aliases {
		args = append(args, "-d", al)
	}
	if _, err := a.Sys.Run("", "certbot", args...); err != nil {
		return bad("certificate request failed. Check that DNS for every name points to this server. %v", err)
	}
	s.SSL, s.ForceHTTPS = true, true
	if err := a.applySite(s); err != nil {
		return err
	}
	if _, err := a.DB.Exec(`UPDATE sites SET ssl=1, force_https=1 WHERE id=?`, id); err != nil {
		return err
	}
	a.audit("ssl.issued", s.Domain)
	writeJSON(w, 200, s)
	return nil
}

func (a *App) disableSSL(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, err := a.loadSite(id)
	if err != nil {
		return err
	}
	s.SSL, s.ForceHTTPS = false, false
	if err := a.applySite(s); err != nil {
		return err
	}
	if _, err := a.DB.Exec(`UPDATE sites SET ssl=0, force_https=0 WHERE id=?`, id); err != nil {
		return err
	}
	a.audit("ssl.disabled", s.Domain)
	writeJSON(w, 200, s)
	return nil
}
