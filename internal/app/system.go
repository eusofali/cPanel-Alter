package app

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

var reService = regexp.MustCompile(`^(nginx|mariadb|postfix|dovecot|pure-ftpd|named|php[0-9]\.[0-9]-fpm)$`)

func (a *App) services() []string {
	list := []string{"nginx", "mariadb"}
	for _, v := range a.phpVersions() {
		list = append(list, "php"+v+"-fpm")
	}
	return append(list, "postfix", "dovecot", "pure-ftpd", "named")
}

func (a *App) phpVersions() []string {
	if a.Sys.Dev {
		return []string{"8.3", "8.2"}
	}
	out := []string{}
	for _, p := range a.Sys.Glob("/etc/php/*/fpm") {
		v := filepath.Base(filepath.Dir(p))
		if rePHP.MatchString(v) {
			out = append(out, v)
		}
	}
	// newest first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func meminfo() (total, available uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		n, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = n * 1024
		case "MemAvailable:":
			available = n * 1024
		}
	}
	return
}

func (a *App) systemInfo(w http.ResponseWriter, r *http.Request) error {
	host, _ := os.Hostname()
	info := map[string]any{"hostname": host, "dev": a.Sys.Dev}

	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) >= 3 {
			info["load"] = f[:3]
		}
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			up, _ := strconv.ParseFloat(f[0], 64)
			info["uptime_seconds"] = int64(up)
		}
	}
	total, avail := meminfo()
	info["mem_total"], info["mem_used"] = total, total-avail

	var st syscall.Statfs_t
	root := a.Sys.Path(WebRoot)
	if syscall.Statfs(root, &st) != nil {
		_ = syscall.Statfs("/", &st)
	}
	dTotal := st.Blocks * uint64(st.Bsize)
	info["disk_total"], info["disk_used"] = dTotal, dTotal-st.Bavail*uint64(st.Bsize)

	type svc struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	svcs := []svc{}
	for _, name := range a.services() {
		status := "simulated"
		if !a.Sys.Dev {
			out, _ := a.Sys.Run("", "systemctl", "is-active", name)
			status = strings.TrimSpace(out)
			if status == "" {
				status = "unknown"
			}
		}
		svcs = append(svcs, svc{name, status})
	}
	info["services"] = svcs

	counts := map[string]int{}
	for key, table := range map[string]string{"sites": "sites", "databases": "dbs", "mailboxes": "mailboxes", "ftp": "ftp", "zones": "dns_zones"} {
		var n int
		_ = a.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
		counts[key] = n
	}
	info["counts"] = counts

	type act struct {
		At     int64  `json:"at"`
		Action string `json:"action"`
		Detail string `json:"detail"`
	}
	acts := []act{}
	rows, err := a.DB.Query(`SELECT at, action, detail FROM activity ORDER BY id DESC LIMIT 50`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var x act
		if err := rows.Scan(&x.At, &x.Action, &x.Detail); err != nil {
			rows.Close()
			return err
		}
		acts = append(acts, x)
	}
	rows.Close()
	info["activity"] = acts

	writeJSON(w, 200, info)
	return nil
}

func (a *App) serviceAction(w http.ResponseWriter, r *http.Request) error {
	name, action := r.PathValue("name"), r.PathValue("action")
	if !reService.MatchString(name) {
		return bad("unknown service")
	}
	if action != "restart" && action != "reload" && action != "start" && action != "stop" {
		return bad("unknown action")
	}
	if _, err := a.Sys.Run("", "systemctl", action, name); err != nil {
		return bad("%v", err)
	}
	a.audit("service."+action, name)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) getSettings(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, map[string]string{"admin_email": a.setting("admin_email"), "server_ip": a.setting("server_ip")})
	return nil
}

func (a *App) putSettings(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		AdminEmail string `json:"admin_email"`
		ServerIP   string `json:"server_ip"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.AdminEmail = strings.ToLower(strings.TrimSpace(in.AdminEmail))
	in.ServerIP = strings.TrimSpace(in.ServerIP)
	if in.AdminEmail != "" && !validEmail(in.AdminEmail) {
		return bad("invalid admin email")
	}
	if in.ServerIP != "" && !validIPv4(in.ServerIP) {
		return bad("server IP must be an IPv4 address")
	}
	if err := a.setSetting("admin_email", in.AdminEmail); err != nil {
		return err
	}
	if err := a.setSetting("server_ip", in.ServerIP); err != nil {
		return err
	}
	a.audit("settings.updated", "panel settings changed")
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
