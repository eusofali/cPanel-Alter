package app

import (
	"regexp"
	"strings"
)

var (
	reDomain    = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)
	reUser      = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)
	reDBName    = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)
	reDBUser    = regexp.MustCompile(`^[a-zA-Z0-9_]{1,32}$`)
	reLocal     = regexp.MustCompile(`^[a-z0-9]([a-z0-9._+-]{0,62}[a-z0-9])?$`)
	rePHP       = regexp.MustCompile(`^[0-9]\.[0-9]$`)
	reProxy     = regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+(:[0-9]{1,5})?$`)
	reNodeEntry = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]{0,120}\.(js|mjs|cjs)$`)
	rePyEntry   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,60}(\.[A-Za-z_][A-Za-z0-9_]{0,60}){0,5}:[A-Za-z_][A-Za-z0-9_]{0,60}$`)
	reDNSName   = regexp.MustCompile(`^(\*|(\*\.)?[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?)*)$`)
	reCAA       = regexp.MustCompile(`^(\d{1,3}) (issue|issuewild|iodef) "([^"\\]*)"$`)
	reSRVValue  = regexp.MustCompile(`^(\d{1,5}) (\d{1,5}) (\S+)$`)
)

func normDomain(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

func validDomain(s string) bool { return len(s) <= 253 && reDomain.MatchString(s) }

func validEmail(s string) bool {
	local, dom, ok := strings.Cut(s, "@")
	return ok && reLocal.MatchString(local) && validDomain(dom)
}

// validPassword rejects short passwords and control characters (which would
// break the line-based config files several services use).
func validPassword(p string) error {
	if len(p) < 8 {
		return bad("password must be at least 8 characters")
	}
	if len(p) > 128 {
		return bad("password must be at most 128 characters")
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return bad("password contains control characters")
		}
	}
	return nil
}
