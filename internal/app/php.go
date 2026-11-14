package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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
	// apt: a distribution package. builtin: compiled into PHP, cannot be
	// removed. vendor: a loader downloaded from its publisher.
	Kind string `json:"kind"`
}

type phpVersion struct {
	Version    string   `json:"version"`
	Extensions []phpExt `json:"extensions"`
	Modules    []string `json:"modules"` // everything PHP reports as loaded
}

// Extensions people expect to find that Debian/Ubuntu compile into the core
// packages, so apt has no separate package for them.
var phpBuiltins = []string{"sodium"}

const sgName = "sourceguardian"

func sgIni(version string) string {
	return "/etc/php/" + version + "/mods-available/" + sgName + ".ini"
}

// loadedModules asks the PHP binary itself what is loaded.
func (a *App) loadedModules(version string) []string {
	if a.Sys.Dev {
		mods := []string{"core", "ctype", "date", "fileinfo", "filter", "hash", "json", "openssl", "pcre", "pdo", "session", "sodium", "standard", "tokenizer", "zlib"}
		for key, on := range a.devExt {
			if v, name, _ := strings.Cut(key, "/"); on && v == version {
				mods = append(mods, name)
			}
		}
		sort.Strings(mods)
		return mods
	}
	out, _ := a.Sys.Run("", "php"+version, "-m")
	seen := map[string]bool{}
	mods := []string{}
	for _, line := range strings.Split(out, "\n") {
		m := strings.ToLower(strings.TrimSpace(line))
		if m == "" || strings.HasPrefix(m, "[") || seen[m] {
			continue
		}
		seen[m] = true
		mods = append(mods, m)
	}
	sort.Strings(mods)
	return mods
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
	list := make([]phpExt, 0, len(state)+2)
	for n, inst := range state {
		list = append(list, phpExt{n, inst, "apt"})
	}
	loaded := map[string]bool{}
	for _, m := range a.loadedModules(version) {
		loaded[m] = true
	}
	for _, name := range phpBuiltins {
		if _, packaged := state[name]; !packaged {
			list = append(list, phpExt{name, loaded[name], "builtin"})
		}
	}
	sg := a.Sys.Exists(sgIni(version))
	if a.Sys.Dev {
		sg = a.devExt[version+"/"+sgName]
	}
	list = append(list, phpExt{sgName, sg, "vendor"})
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

func (a *App) listPHP(w http.ResponseWriter, r *http.Request) error {
	out := []phpVersion{}
	for _, v := range a.phpVersions() {
		out = append(out, phpVersion{v, a.phpExtensions(v), a.loadedModules(v)})
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
	kind := ""
	for _, e := range a.phpExtensions(version) {
		if e.Name == in.Name {
			kind = e.Kind
			if e.Kind == "builtin" && e.Installed {
				return bad("%s is built into PHP %s and is always enabled", in.Name, version)
			}
		}
	}
	switch kind {
	case "":
		return bad("extension %s is not available for PHP %s", in.Name, version)
	case "builtin":
		return bad("%s is not part of this PHP %s build and has no package to install", in.Name, version)
	case "vendor":
		if a.Sys.Dev {
			if a.devExt == nil {
				a.devExt = map[string]bool{}
			}
			a.devExt[version+"/"+in.Name] = in.Install
		}
		if err := a.setSourceGuardian(version, in.Install); err != nil {
			return err
		}
		a.audit("php.extension.vendor", fmt.Sprintf("%s for PHP %s (install=%v)", in.Name, version, in.Install))
		writeJSON(w, 200, map[string]bool{"ok": true})
		return nil
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

// SourceGuardian is a commercial loader that is not in any apt repository.
// The publisher ships one archive per platform holding a loader file for
// each PHP version (ixed.<version>.lin). If the publisher moves it, set
// HOSTDECK_SOURCEGUARDIAN_URL in the panel's systemd unit.
const sgDefaultURL = "https://www.sourceguardian.com/loaders/download/loaders.linux-%s.tar.gz"

func (a *App) setSourceGuardian(version string, install bool) error {
	ini := sgIni(version)
	loader := "ixed." + version + ".lin"
	fpm := "php" + version + "-fpm"
	if !install {
		_, _ = a.Sys.Run("", "phpdismod", "-v", version, sgName)
		if err := a.Sys.Remove(ini); err != nil {
			return err
		}
		if _, err := a.Sys.Run("", "systemctl", "restart", fpm); err != nil {
			return bad("%v", err)
		}
		return nil
	}

	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if arch == "" {
		return bad("SourceGuardian has no loader for this CPU architecture (%s)", runtime.GOARCH)
	}
	url := os.Getenv("HOSTDECK_SOURCEGUARDIAN_URL")
	if url == "" {
		url = fmt.Sprintf(sgDefaultURL, arch)
	}
	if !a.Sys.Dev {
		tmp, err := os.MkdirTemp("", "hostdeck-sg-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		archive := filepath.Join(tmp, "loaders.tar.gz")
		if _, err := a.Sys.RunTimeout(5*time.Minute, "", "curl", "-fsSL", "--proto", "=https", "--max-time", "240", "-o", archive, url); err != nil {
			return bad("could not download the SourceGuardian loaders from %s: %v", url, err)
		}
		unpack := filepath.Join(tmp, "x")
		if err := os.Mkdir(unpack, 0o700); err != nil {
			return err
		}
		// Only the one loader file is extracted, and only if it is a plain file.
		_, _ = a.Sys.Run("", "tar", "-xzf", archive, "-C", unpack, "--no-same-owner", "--no-same-permissions", "--wildcards", "*"+loader)
		found := ""
		_ = filepath.WalkDir(unpack, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() && d.Name() == loader {
				found = p
			}
			return nil
		})
		if found == "" {
			return bad("the SourceGuardian download has no loader for PHP %s yet", version)
		}
		extDir, err := a.Sys.Run("", "php"+version, "-r", `echo ini_get("extension_dir");`)
		extDir = strings.TrimSpace(extDir)
		if err != nil || !filepath.IsAbs(extDir) {
			return bad("could not find the PHP %s extension directory", version)
		}
		if _, err := a.Sys.Run("", "install", "-o", "root", "-g", "root", "-m", "644", found, filepath.Join(extDir, loader)); err != nil {
			return bad("%v", err)
		}
	}
	if err := a.Sys.WriteFile(ini, []byte("; Managed by hostdeck.\n; priority=15\nextension="+loader+"\n"), 0o644); err != nil {
		return err
	}
	if _, err := a.Sys.Run("", "phpenmod", "-v", version, sgName); err != nil {
		return bad("%v", err)
	}
	// If PHP cannot start with the loader, take it out again.
	if _, err := a.Sys.Run("", "php-fpm"+version, "-t"); err != nil {
		_, _ = a.Sys.Run("", "phpdismod", "-v", version, sgName)
		_ = a.Sys.Remove(ini)
		return bad("PHP %s rejected the SourceGuardian loader, so it was removed again: %v", version, err)
	}
	if _, err := a.Sys.Run("", "systemctl", "restart", fpm); err != nil {
		return bad("%v", err)
	}
	return nil
}
