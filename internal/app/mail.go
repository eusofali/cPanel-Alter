package app

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	postfixDir   = "/etc/postfix/hmea-panel"
	dovecotUsers = "/etc/dovecot/hmea-panel-users"
	mailBase     = "/var/mail/vhosts"
	vmailID      = 5000 // uid/gid of the "vmail" user created by the installer
)

type mailbox struct {
	ID      int64  `json:"id"`
	Address string `json:"address"`
	Local   string `json:"local"`
	QuotaMB int    `json:"quota_mb"`
}

type mailAlias struct {
	ID          int64  `json:"id"`
	Source      string `json:"source"` // "*" is a catch-all
	Destination string `json:"destination"`
}

type mailDomain struct {
	ID        int64       `json:"id"`
	Domain    string      `json:"domain"`
	Mailboxes []mailbox   `json:"mailboxes"`
	Aliases   []mailAlias `json:"aliases"`
}

func (a *App) loadMail() ([]mailDomain, error) {
	rows, err := a.DB.Query(`SELECT id, domain FROM mail_domains ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	domains := []mailDomain{}
	index := map[int64]int{}
	for rows.Next() {
		d := mailDomain{Mailboxes: []mailbox{}, Aliases: []mailAlias{}}
		if err := rows.Scan(&d.ID, &d.Domain); err != nil {
			rows.Close()
			return nil, err
		}
		index[d.ID] = len(domains)
		domains = append(domains, d)
	}
	rows.Close()

	rows, err = a.DB.Query(`SELECT id, domain_id, local, quota_mb FROM mailboxes ORDER BY local`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m mailbox
		var did int64
		if err := rows.Scan(&m.ID, &did, &m.Local, &m.QuotaMB); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := index[did]; ok {
			m.Address = m.Local + "@" + domains[i].Domain
			domains[i].Mailboxes = append(domains[i].Mailboxes, m)
		}
	}
	rows.Close()

	rows, err = a.DB.Query(`SELECT id, domain_id, source, destination FROM mail_aliases ORDER BY source`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var al mailAlias
		var did int64
		if err := rows.Scan(&al.ID, &did, &al.Source, &al.Destination); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := index[did]; ok {
			domains[i].Aliases = append(domains[i].Aliases, al)
		}
	}
	rows.Close()
	return domains, nil
}

// renderMail produces the Postfix lookup tables and the Dovecot passwd-file.
func renderMail(domains []mailDomain, hashes map[int64]string) (vdomains, vmailbox, valias, users string) {
	var d, mb, al, u strings.Builder
	for _, dom := range domains {
		fmt.Fprintf(&d, "%s OK\n", dom.Domain)
		for _, m := range dom.Mailboxes {
			fmt.Fprintf(&mb, "%s %s/%s/\n", m.Address, dom.Domain, m.Local)
			fmt.Fprintf(&u, "%s:{BLF-CRYPT}%s:%d:%d::%s/%s/%s::userdb_quota_rule=*:storage=%dM\n",
				m.Address, hashes[m.ID], vmailID, vmailID, mailBase, dom.Domain, m.Local, m.QuotaMB)
		}
		for _, x := range dom.Aliases {
			src := x.Source + "@" + dom.Domain
			if x.Source == "*" {
				src = "@" + dom.Domain
			}
			fmt.Fprintf(&al, "%s %s\n", src, x.Destination)
		}
	}
	return d.String(), mb.String(), al.String(), u.String()
}

func (a *App) applyMail() error {
	domains, err := a.loadMail()
	if err != nil {
		return err
	}
	hashes := map[int64]string{}
	rows, err := a.DB.Query(`SELECT id, hash FROM mailboxes`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var h string
		if err := rows.Scan(&id, &h); err != nil {
			rows.Close()
			return err
		}
		hashes[id] = h
	}
	rows.Close()

	vdomains, vmailbox, valias, users := renderMail(domains, hashes)
	files := map[string]string{"vdomains": vdomains, "vmailbox": vmailbox, "valias": valias}
	for name, content := range files {
		if err := a.Sys.WriteFile(postfixDir+"/"+name, []byte(content), 0o644); err != nil {
			return err
		}
		if _, err := a.Sys.Run("", "postmap", postfixDir+"/"+name); err != nil {
			return bad("%v", err)
		}
	}
	if err := a.Sys.WriteFile(dovecotUsers, []byte(users), 0o640); err != nil {
		return err
	}
	// Dovecot must be able to read its user file but nobody else should.
	if _, err := a.Sys.Run("", "chown", "root:dovecot", dovecotUsers); err != nil {
		return bad("%v", err)
	}
	if _, err := a.Sys.Run("", "systemctl", "reload", "postfix"); err != nil {
		return bad("%v", err)
	}
	return nil
}

func (a *App) listMail(w http.ResponseWriter, r *http.Request) error {
	domains, err := a.loadMail()
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"domains": domains})
	return nil
}

func (a *App) mailDomainName(id int64) (string, error) {
	var d string
	if a.DB.QueryRow(`SELECT domain FROM mail_domains WHERE id=?`, id).Scan(&d) != nil {
		return "", notFound("mail domain")
	}
	return d, nil
}

func (a *App) createMailDomain(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Domain string `json:"domain"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	domain := normDomain(in.Domain)
	if !validDomain(domain) {
		return bad("invalid domain name")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.DB.Exec(`INSERT INTO mail_domains(domain) VALUES(?)`, domain); isUnique(err) {
		return conflict("mail domain %s already exists", domain)
	} else if err != nil {
		return err
	}
	if err := a.applyMail(); err != nil {
		return err
	}
	a.audit("mail.domain.created", domain)
	writeJSON(w, 201, map[string]bool{"ok": true})
	return nil
}

func (a *App) deleteMailDomain(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	domain, err := a.mailDomainName(id)
	if err != nil {
		return err
	}
	if _, err := a.DB.Exec(`DELETE FROM mail_domains WHERE id=?`, id); err != nil {
		return err
	}
	if err := a.applyMail(); err != nil {
		return err
	}
	a.audit("mail.domain.deleted", domain)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func validQuota(q int) bool { return q >= 10 && q <= 1024*1024 }

func (a *App) createMailbox(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		DomainID int64  `json:"domain_id"`
		Local    string `json:"local"`
		Password string `json:"password"`
		QuotaMB  int    `json:"quota_mb"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Local = strings.ToLower(strings.TrimSpace(in.Local))
	if !reLocal.MatchString(in.Local) {
		return bad("invalid mailbox name")
	}
	if err := validPassword(in.Password); err != nil {
		return err
	}
	if in.QuotaMB == 0 {
		in.QuotaMB = 1024
	}
	if !validQuota(in.QuotaMB) {
		return bad("quota must be between 10 MB and 1 TB")
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	domain, err := a.mailDomainName(in.DomainID)
	if err != nil {
		return err
	}
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM mail_aliases WHERE domain_id=? AND source=?`, in.DomainID, in.Local).Scan(&n)
	if n > 0 {
		return conflict("%s@%s is already a forwarder", in.Local, domain)
	}
	if _, err := a.DB.Exec(`INSERT INTO mailboxes(domain_id, local, hash, quota_mb) VALUES(?,?,?,?)`, in.DomainID, in.Local, hash, in.QuotaMB); isUnique(err) {
		return conflict("mailbox %s@%s already exists", in.Local, domain)
	} else if err != nil {
		return err
	}
	if err := a.applyMail(); err != nil {
		return err
	}
	a.audit("mailbox.created", in.Local+"@"+domain)
	writeJSON(w, 201, map[string]bool{"ok": true})
	return nil
}

func (a *App) updateMailbox(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct {
		Password string `json:"password"`
		QuotaMB  int    `json:"quota_mb"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !validQuota(in.QuotaMB) {
		return bad("quota must be between 10 MB and 1 TB")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var local string
	if a.DB.QueryRow(`SELECT local FROM mailboxes WHERE id=?`, id).Scan(&local) != nil {
		return notFound("mailbox")
	}
	if in.Password != "" {
		if err := validPassword(in.Password); err != nil {
			return err
		}
		hash, err := hashPassword(in.Password)
		if err != nil {
			return err
		}
		if _, err := a.DB.Exec(`UPDATE mailboxes SET hash=? WHERE id=?`, hash, id); err != nil {
			return err
		}
	}
	if _, err := a.DB.Exec(`UPDATE mailboxes SET quota_mb=? WHERE id=?`, in.QuotaMB, id); err != nil {
		return err
	}
	if err := a.applyMail(); err != nil {
		return err
	}
	a.audit("mailbox.updated", local)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) deleteMailbox(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var local string
	if a.DB.QueryRow(`SELECT local FROM mailboxes WHERE id=?`, id).Scan(&local) != nil {
		return notFound("mailbox")
	}
	if _, err := a.DB.Exec(`DELETE FROM mailboxes WHERE id=?`, id); err != nil {
		return err
	}
	if err := a.applyMail(); err != nil {
		return err
	}
	a.audit("mailbox.deleted", local)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) createAlias(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		DomainID    int64  `json:"domain_id"`
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Source = strings.ToLower(strings.TrimSpace(in.Source))
	if in.Source != "*" && !reLocal.MatchString(in.Source) {
		return bad("invalid forwarder name (use * for a catch-all)")
	}
	dests := []string{}
	for _, d := range strings.FieldsFunc(strings.ToLower(in.Destination), func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if !validEmail(d) {
			return bad("invalid destination address %q", d)
		}
		dests = append(dests, d)
	}
	if len(dests) == 0 || len(dests) > 20 {
		return bad("give between 1 and 20 destination addresses")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	domain, err := a.mailDomainName(in.DomainID)
	if err != nil {
		return err
	}
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM mailboxes WHERE domain_id=? AND local=?`, in.DomainID, in.Source).Scan(&n)
	if n > 0 {
		return conflict("%s@%s is already a mailbox", in.Source, domain)
	}
	if _, err := a.DB.Exec(`INSERT INTO mail_aliases(domain_id, source, destination) VALUES(?,?,?)`, in.DomainID, in.Source, strings.Join(dests, ",")); isUnique(err) {
		return conflict("forwarder %s@%s already exists", in.Source, domain)
	} else if err != nil {
		return err
	}
	if err := a.applyMail(); err != nil {
		return err
	}
	a.audit("mail.alias.created", in.Source+"@"+domain)
	writeJSON(w, 201, map[string]bool{"ok": true})
	return nil
}

func (a *App) deleteAlias(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	res, err := a.DB.Exec(`DELETE FROM mail_aliases WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("forwarder")
	}
	if err := a.applyMail(); err != nil {
		return err
	}
	a.audit("mail.alias.deleted", fmt.Sprint(id))
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
