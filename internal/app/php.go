package app

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

var reExt = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)

// Packages that are not loadable extensions, or that the panel itself needs.
var phpNotExtensions = map[string]bool{"fpm": true, "cli": true, "cgi": true, "common": true, "dev": true, "dbg": true, "phpdbg": true, "embed": true, "litespeed": true}
var phpProtected = map[string]bool{"fpm": true, "common": true, "cli": true}

var devCatalog = []string{"apcu", "bcmath", "bz2", "curl", "gd", "gmp", "igbinary", "imagick", "imap", "intl", "ldap", "mbstring", "memcached", "mongodb", "mysql", "opcache", "pgsql", "readline", "redis", "soap", "sqlite3", "xdebug", "xml", "xsl", "zip"}

type phpExt struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

type phpVersion struct {
	Version    string   `json:"version"`
	Extensions []phpExt `json:"extensions"`
}

func (a *App) phpExtensions(version string) []phpExt {
	prefix := "php" + version + "-"
	state := map[string]bool{}
	if a.Sys.Dev {
		for _, n := range devCatalog {
			state[n] = a.devExt[version+"/"+n]
		}
	} else {
		// Everything the configured apt sources offer for this version...
		out, _ := a.Sys.Run("", "apt-cache", "pkgnames", prefix)
		for _, pkg := range strings.Fields(out) {
			name := strings.TrimPrefix(pkg, prefix)
			if reExt.MatchString(name) && !phpNotExtensions[name] {
				state[name] = false
			}
		}
		// ...and which of those are installed.
		out, _ = a.Sys.Run("", "dpkg-query", "-W", "-f", "${Package} ${db:Status-Abbrev}\n", prefix+"*")
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && strings.HasPrefix(f[1], "ii") {
				name := strings.TrimPrefix(f[0], prefix)
				if reExt.MatchString(name) && !phpNotExtensions[name] {
					state[name] = true
				}
			}
		}
	}
	list := make([]phpExt, 0, len(state))
	for n, inst := range state {
		list = append(list, phpExt{n, inst})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

func (a *App) listPHP(w http.ResponseWriter, r *http.Request) error {
	out := []phpVersion{}
	for _, v := range a.phpVersions() {
		out = append(out, phpVersion{v, a.phpExtensions(v)})
	}
	writeJSON(w, 200, map[string]any{"versions": out})
	return nil
}

func (a *App) setPHPExtension(w http.ResponseWriter, r *http.Request) error {
	version := r.PathValue("version")
	var in struct {
		Name    string `json:"name"`
		Install bool   `json:"install"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	known := false
	for _, v := range a.phpVersions() {
		known = known || v == version
	}
	if !known {
		return bad("PHP %s is not installed", version)
	}
	if !reExt.MatchString(in.Name) || phpNotExtensions[in.Name] || phpProtected[in.Name] {
		return bad("invalid extension name")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// Only names apt actually offers are accepted, never free-form packages.
	available := false
	for _, e := range a.phpExtensions(version) {
		available = available || e.Name == in.Name
	}
	if !available {
		return bad("extension %s is not available for PHP %s", in.Name, version)
	}
	pkg := "php" + version + "-" + in.Name
	verb := "install"
	if !in.Install {
		verb = "remove"
	}
	if a.Sys.Dev {
		if a.devExt == nil {
			a.devExt = map[string]bool{}
		}
		a.devExt[version+"/"+in.Name] = in.Install
	}
	if _, err := a.Sys.RunTimeout(10*time.Minute, "", "apt-get", verb, "-y", "--no-install-recommends", pkg); err != nil {
		return bad("%v", err)
	}
	if _, err := a.Sys.Run("", "systemctl", "restart", "php"+version+"-fpm"); err != nil {
		return bad("%v", err)
	}
	a.audit("php.extension."+verb, pkg)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
