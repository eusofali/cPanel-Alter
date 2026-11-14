package app

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"text/template"
	"time"
)

// Node.js and Python sites run as a systemd service owned by the panel and
// are reached through Nginx on a private local port.
const appPortBase = 20000

func (s Site) isApp() bool { return s.Type == "node" || s.Type == "python" }

func unitName(id int64) string    { return fmt.Sprintf("hostdeck-site%d", id) }
func unitFile(id int64) string    { return "/etc/systemd/system/" + unitName(id) + ".service" }
func appDir(domain string) string { return siteDir(domain) + "/app" }

var unitTmpl = template.Must(template.New("unit").Parse(`# Managed by hostdeck. Changes made here are overwritten.
[Unit]
Description=hostdeck app for {{.Domain}}
After=network.target

[Service]
User=www-data
Group=www-data
WorkingDirectory={{.Dir}}/app
Environment=PORT={{.Port}} HOST=127.0.0.1 NODE_ENV=production PYTHONUNBUFFERED=1 HOME={{.Dir}}
ExecStart={{.Exec}}
Restart=on-failure
RestartSec=3
# The app may only write inside its own site folder.
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths={{.Dir}}
MemoryMax=512M

[Install]
WantedBy=multi-user.target
`))

func renderUnit(s Site) (string, error) {
	dir := siteDir(s.Domain)
	exec := "/usr/bin/node " + s.Entry
	if s.Type == "python" {
		exec = fmt.Sprintf("%s/venv/bin/gunicorn --workers 2 --bind 127.0.0.1:%d %s", dir, s.Port, s.Entry)
	}
	var buf bytes.Buffer
	err := unitTmpl.Execute(&buf, map[string]any{"Domain": s.Domain, "Dir": dir, "Port": s.Port, "Exec": exec})
	return buf.String(), err
}

// runtimes reports the installed interpreter versions ("" when missing).
func (a *App) runtimes() map[string]string {
	if a.Sys.Dev {
		return map[string]string{"node": "v22 (simulated)", "python": "3.12 (simulated)"}
	}
	out := map[string]string{"node": "", "python": ""}
	if v, err := a.Sys.Run("", "node", "--version"); err == nil {
		out["node"] = strings.TrimSpace(v)
	}
	if v, err := a.Sys.Run("", "python3", "--version"); err == nil {
		out["python"] = strings.TrimPrefix(strings.TrimSpace(v), "Python ")
	}
	return out
}

// asWeb runs a command as www-data with the site folder as its home, so
// package managers keep their caches inside the site and never run as root.
func (a *App) asWeb(limit time.Duration, s Site, name string, args ...string) (string, error) {
	dir := siteDir(s.Domain)
	full := append([]string{"-u", "www-data", "--", "env", "HOME=" + dir, "npm_config_cache=" + dir + "/tmp/.npm", "PIP_CACHE_DIR=" + dir + "/tmp/.pip", name}, args...)
	return a.Sys.RunTimeout(limit, "", "runuser", full...)
}

const nodeStarter = `// Starter app created by hostdeck. Replace it with your own code.
const http = require("http");
const port = process.env.PORT || 3000;
http
  .createServer((req, res) => {
    res.writeHead(200, { "Content-Type": "text/plain; charset=utf-8" });
    res.end("Node.js app is running on %DOMAIN%\n");
  })
  .listen(port, "127.0.0.1");
`

const pythonStarter = `# Starter app created by hostdeck. Replace it with your own code.
# Flask or Django work the same way: point the site's WSGI entry at them.
def app(environ, start_response):
    start_response("200 OK", [("Content-Type", "text/plain; charset=utf-8")])
    return [b"Python app is running on %DOMAIN%\n"]
`

// prepareApp creates the app folder, a starter file when the entry does not
// exist yet, and (for Python) a virtualenv with gunicorn.
func (a *App) prepareApp(s Site) error {
	if !s.isApp() {
		return nil
	}
	dir := appDir(s.Domain)
	if err := a.Sys.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := a.Sys.MkdirAll(siteDir(s.Domain)+"/tmp", 0o750); err != nil {
		return err
	}
	if s.Type == "node" {
		if !a.Sys.Exists(dir + "/" + s.Entry) {
			if err := a.Sys.WriteFile(dir+"/"+s.Entry, []byte(strings.ReplaceAll(nodeStarter, "%DOMAIN%", s.Domain)), 0o644); err != nil {
				return err
			}
		}
	} else {
		module, _, _ := strings.Cut(s.Entry, ":")
		file := dir + "/" + strings.ReplaceAll(module, ".", "/")
		if !a.Sys.Exists(file+".py") && !a.Sys.Exists(file+"/__init__.py") {
			if err := a.Sys.WriteFile(file+".py", []byte(strings.ReplaceAll(pythonStarter, "%DOMAIN%", s.Domain)), 0o644); err != nil {
				return err
			}
		}
	}
	if _, err := a.Sys.Run("", "chown", "-R", "www-data:www-data", siteDir(s.Domain)); err != nil {
		return bad("%v", err)
	}
	if s.Type == "python" && !a.Sys.Exists(siteDir(s.Domain)+"/venv/bin/gunicorn") {
		venv := siteDir(s.Domain) + "/venv"
		if _, err := a.asWeb(5*time.Minute, s, "python3", "-m", "venv", venv); err != nil {
			return bad("could not create the Python environment: %v", err)
		}
		if _, err := a.asWeb(5*time.Minute, s, venv+"/bin/pip", "install", "--quiet", "gunicorn"); err != nil {
			return bad("could not install gunicorn: %v", err)
		}
	}
	return nil
}

// applyApp makes the systemd unit match the site: present and running for an
// enabled app, stopped and removed otherwise.
func (a *App) applyApp(s Site) error {
	path := unitFile(s.ID)
	previous, prevErr := a.Sys.ReadFile(path)
	if !(s.Enabled && s.isApp()) {
		if prevErr != nil {
			return nil
		}
		_, _ = a.Sys.Run("", "systemctl", "disable", "--now", unitName(s.ID))
		if err := a.Sys.Remove(path); err != nil {
			return err
		}
		_, err := a.Sys.Run("", "systemctl", "daemon-reload")
		return err
	}
	unit, err := renderUnit(s)
	if err != nil {
		return err
	}
	if prevErr == nil && string(previous) == unit {
		return nil
	}
	if err := a.Sys.WriteFile(path, []byte(unit), 0o644); err != nil {
		return err
	}
	if _, err := a.Sys.Run("", "systemctl", "daemon-reload"); err != nil {
		return bad("%v", err)
	}
	if _, err := a.Sys.Run("", "systemctl", "enable", unitName(s.ID)); err != nil {
		return bad("%v", err)
	}
	if _, err := a.Sys.Run("", "systemctl", "restart", unitName(s.ID)); err != nil {
		return bad("the app did not start: %v", err)
	}
	return nil
}

func (a *App) appSite(r *http.Request) (Site, error) {
	id, err := pathID(r)
	if err != nil {
		return Site{}, err
	}
	s, err := a.loadSite(id)
	if err != nil {
		return s, err
	}
	if !s.isApp() {
		return s, bad("%s is not a Node.js or Python app", s.Domain)
	}
	return s, nil
}

func (a *App) restartApp(w http.ResponseWriter, r *http.Request) error {
	s, err := a.appSite(r)
	if err != nil {
		return err
	}
	if !s.Enabled {
		return bad("enable the site first")
	}
	if _, err := a.Sys.Run("", "systemctl", "restart", unitName(s.ID)); err != nil {
		return bad("%v", err)
	}
	a.audit("app.restarted", s.Domain)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

// installAppDeps runs npm or pip for the app, as www-data, then restarts it.
func (a *App) installAppDeps(w http.ResponseWriter, r *http.Request) error {
	s, err := a.appSite(r)
	if err != nil {
		return err
	}
	dir := appDir(s.Domain)
	var out string
	if s.Type == "node" {
		if !a.Sys.Exists(dir + "/package.json") {
			return bad("upload a package.json to the app folder first")
		}
		out, err = a.asWeb(10*time.Minute, s, "npm", "--prefix", dir, "install", "--omit=dev", "--no-audit", "--no-fund")
	} else {
		if !a.Sys.Exists(dir + "/requirements.txt") {
			return bad("upload a requirements.txt to the app folder first")
		}
		out, err = a.asWeb(10*time.Minute, s, siteDir(s.Domain)+"/venv/bin/pip", "install", "-r", dir+"/requirements.txt")
	}
	if err != nil {
		return bad("%v", err)
	}
	if s.Enabled {
		if _, err := a.Sys.Run("", "systemctl", "restart", unitName(s.ID)); err != nil {
			return bad("dependencies installed, but the app did not restart: %v", err)
		}
	}
	a.audit("app.dependencies", s.Domain)
	writeJSON(w, 200, map[string]string{"output": out})
	return nil
}

func (a *App) appLogs(w http.ResponseWriter, r *http.Request) error {
	s, err := a.appSite(r)
	if err != nil {
		return err
	}
	out, _ := a.Sys.Run("", "journalctl", "-u", unitName(s.ID), "-n", "200", "--no-pager", "-o", "short-iso")
	if a.Sys.Dev {
		out = "Development mode: the app is not actually running, so there are no logs."
	}
	status, _ := a.Sys.Run("", "systemctl", "is-active", unitName(s.ID))
	if a.Sys.Dev {
		status = "simulated"
	}
	writeJSON(w, 200, map[string]string{"logs": out, "status": strings.TrimSpace(status)})
	return nil
}
