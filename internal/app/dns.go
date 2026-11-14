package app

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	bindDir  = "/etc/bind/hmea-panel"
	bindConf = "/etc/bind/hmea-panel/zones.conf"
)

var recordTypes = map[string]bool{"A": true, "AAAA": true, "CNAME": true, "MX": true, "TXT": true, "NS": true, "SRV": true, "CAA": true}

type dnsRecord struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	TTL   int    `json:"ttl"`
	Prio  int    `json:"prio"`
	Value string `json:"value"`
}

type dnsZone struct {
	ID      int64       `json:"id"`
	Domain  string      `json:"domain"`
	Serial  int64       `json:"serial"`
	Records []dnsRecord `json:"records"`
}

func validIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil && !strings.Contains(s, ":")
}

func validIPv6(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && strings.Contains(s, ":")
}

// normalizeRecord validates a record and rewrites it into canonical form.
// Host targets are stored as fully qualified names without the final dot.
func normalizeRecord(zone string, rec *dnsRecord) error {
	rec.Type = strings.ToUpper(strings.TrimSpace(rec.Type))
	if !recordTypes[rec.Type] {
		return bad("unsupported record type")
	}
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(rec.Name)), ".")
	switch {
	case name == "" || name == zone:
		name = "@"
	case strings.HasSuffix(name, "."+zone):
		name = strings.TrimSuffix(name, "."+zone)
	}
	if name != "@" && (!reDNSName.MatchString(name) || len(name)+len(zone) > 252) {
		return bad("invalid record name")
	}
	rec.Name = name
	if rec.TTL == 0 {
		rec.TTL = 3600
	}
	if rec.TTL < 60 || rec.TTL > 604800 {
		return bad("TTL must be between 60 and 604800 seconds")
	}
	if rec.Prio < 0 || rec.Prio > 65535 {
		return bad("priority must be between 0 and 65535")
	}
	value := strings.TrimSpace(rec.Value)
	host := func(v string) (string, error) {
		v = normDomain(v)
		if v == "@" {
			return zone, nil
		}
		if !validDomain(v) {
			return "", bad("%s target must be a full host name such as mail.%s", rec.Type, zone)
		}
		return v, nil
	}
	var err error
	switch rec.Type {
	case "A":
		if !validIPv4(value) {
			return bad("A record needs an IPv4 address")
		}
	case "AAAA":
		if !validIPv6(value) {
			return bad("AAAA record needs an IPv6 address")
		}
		value = net.ParseIP(value).String()
	case "CNAME":
		if name == "@" {
			return bad("a CNAME cannot be set on the zone itself")
		}
		value, err = host(value)
	case "NS", "MX":
		value, err = host(value)
	case "TXT":
		if value == "" || len(value) > 4096 {
			return bad("TXT value must be 1-4096 characters")
		}
		for _, r := range value {
			if r < 0x20 || r > 0x7e {
				return bad("TXT value must be printable ASCII")
			}
		}
	case "SRV":
		m := reSRVValue.FindStringSubmatch(value)
		if m == nil {
			return bad(`SRV value must be "weight port target"`)
		}
		w, _ := strconv.Atoi(m[1])
		p, _ := strconv.Atoi(m[2])
		if w > 65535 || p > 65535 {
			return bad("SRV weight and port must be at most 65535")
		}
		var target string
		if target, err = host(m[3]); err == nil {
			value = fmt.Sprintf("%d %d %s", w, p, target)
		}
	case "CAA":
		m := reCAA.FindStringSubmatch(value)
		if m == nil {
			return bad(`CAA value must look like: 0 issue "letsencrypt.org"`)
		}
		if f, _ := strconv.Atoi(m[1]); f > 255 {
			return bad("CAA flag must be at most 255")
		}
	}
	if err != nil {
		return err
	}
	if rec.Type != "MX" && rec.Type != "SRV" {
		rec.Prio = 0
	}
	rec.Value = value
	return nil
}

func txtChunks(v string) string {
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace
	var parts []string
	for len(v) > 255 {
		parts = append(parts, `"`+esc(v[:255])+`"`)
		v = v[255:]
	}
	parts = append(parts, `"`+esc(v)+`"`)
	return strings.Join(parts, " ")
}

func renderZone(z dnsZone) string {
	primary := "ns1." + z.Domain
	for _, r := range z.Records {
		if r.Type == "NS" && r.Name == "@" {
			primary = r.Value
			break
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "; Managed by hmea-panel. Changes made here are overwritten.\n$ORIGIN %s.\n$TTL 3600\n", z.Domain)
	fmt.Fprintf(&b, "@ IN SOA %s. hostmaster.%s. ( %d 3600 900 1209600 300 )\n", primary, z.Domain, z.Serial)
	for _, r := range z.Records {
		var data string
		switch r.Type {
		case "CNAME", "NS":
			data = r.Value + "."
		case "MX":
			data = fmt.Sprintf("%d %s.", r.Prio, r.Value)
		case "SRV":
			data = fmt.Sprintf("%d %s.", r.Prio, r.Value)
		case "TXT":
			data = txtChunks(r.Value)
		default:
			data = r.Value
		}
		fmt.Fprintf(&b, "%s %d IN %s %s\n", r.Name, r.TTL, r.Type, data)
	}
	return b.String()
}

func (a *App) loadZone(id int64) (dnsZone, error) {
	z := dnsZone{ID: id, Records: []dnsRecord{}}
	if a.DB.QueryRow(`SELECT domain, serial FROM dns_zones WHERE id=?`, id).Scan(&z.Domain, &z.Serial) != nil {
		return z, notFound("zone")
	}
	rows, err := a.DB.Query(`SELECT id, name, type, ttl, prio, value FROM dns_records WHERE zone_id=? ORDER BY CASE type WHEN 'NS' THEN 0 WHEN 'A' THEN 1 WHEN 'AAAA' THEN 2 WHEN 'CNAME' THEN 3 WHEN 'MX' THEN 4 ELSE 5 END, name, id`, id)
	if err != nil {
		return z, err
	}
	defer rows.Close()
	for rows.Next() {
		var r dnsRecord
		if err := rows.Scan(&r.ID, &r.Name, &r.Type, &r.TTL, &r.Prio, &r.Value); err != nil {
			return z, err
		}
		z.Records = append(z.Records, r)
	}
	return z, rows.Err()
}

// nextSerial follows the YYYYMMDDnn convention and always increases.
func nextSerial(current int64, now time.Time) int64 {
	today, _ := strconv.ParseInt(now.UTC().Format("20060102")+"00", 10, 64)
	if current >= today {
		return current + 1
	}
	return today
}

func (a *App) applyZoneList() error {
	rows, err := a.DB.Query(`SELECT domain FROM dns_zones ORDER BY domain`)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("// Managed by hmea-panel. Changes made here are overwritten.\n")
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return err
		}
		fmt.Fprintf(&b, "zone \"%s\" { type master; file \"%s/db.%s\"; };\n", d, bindDir, d)
	}
	rows.Close()
	return a.Sys.WriteFile(bindConf, []byte(b.String()), 0o644)
}

// applyZone bumps the serial, writes the zone file, lets BIND check it and
// restores the previous file if the check fails.
func (a *App) applyZone(id int64) error {
	z, err := a.loadZone(id)
	if err != nil {
		return err
	}
	oldSerial := z.Serial
	z.Serial = nextSerial(z.Serial, time.Now())
	path := bindDir + "/db." + z.Domain
	previous, prevErr := a.Sys.ReadFile(path)
	if err := a.Sys.WriteFile(path, []byte(renderZone(z)), 0o644); err != nil {
		return err
	}
	if _, err := a.Sys.Run("", "named-checkzone", z.Domain, path); err != nil {
		if prevErr == nil {
			_ = a.Sys.WriteFile(path, previous, 0o644)
		} else {
			_ = a.Sys.Remove(path)
		}
		return bad("BIND rejected the zone: %v", err)
	}
	if _, err := a.DB.Exec(`UPDATE dns_zones SET serial=? WHERE id=? AND serial=?`, z.Serial, id, oldSerial); err != nil {
		return err
	}
	if err := a.applyZoneList(); err != nil {
		return err
	}
	if _, err := a.Sys.Run("", "rndc", "reload"); err != nil {
		return bad("%v", err)
	}
	return nil
}

func (a *App) listZones(w http.ResponseWriter, r *http.Request) error {
	rows, err := a.DB.Query(`SELECT z.id, z.domain, z.serial, (SELECT COUNT(*) FROM dns_records WHERE zone_id=z.id) FROM dns_zones z ORDER BY z.domain`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		ID      int64  `json:"id"`
		Domain  string `json:"domain"`
		Serial  int64  `json:"serial"`
		Records int    `json:"records"`
	}
	out := []row{}
	for rows.Next() {
		var z row
		if err := rows.Scan(&z.ID, &z.Domain, &z.Serial, &z.Records); err != nil {
			return err
		}
		out = append(out, z)
	}
	writeJSON(w, 200, map[string]any{"zones": out, "server_ip": a.setting("server_ip")})
	return nil
}

func (a *App) getZone(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	z, err := a.loadZone(id)
	if err != nil {
		return err
	}
	writeJSON(w, 200, z)
	return nil
}

func (a *App) createZone(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Domain string `json:"domain"`
		IP     string `json:"ip"`
		Mail   bool   `json:"mail"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	domain := normDomain(in.Domain)
	if !validDomain(domain) {
		return bad("invalid domain name")
	}
	ip := strings.TrimSpace(in.IP)
	if !validIPv4(ip) {
		return bad("enter the server's IPv4 address")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	res, err := a.DB.Exec(`INSERT INTO dns_zones(domain, serial) VALUES(?, 0)`, domain)
	if isUnique(err) {
		return conflict("zone %s already exists", domain)
	} else if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	defaults := []dnsRecord{
		{Name: "@", Type: "NS", Value: "ns1." + domain}, {Name: "@", Type: "NS", Value: "ns2." + domain},
		{Name: "@", Type: "A", Value: ip}, {Name: "www", Type: "A", Value: ip},
		{Name: "ns1", Type: "A", Value: ip}, {Name: "ns2", Type: "A", Value: ip},
	}
	if in.Mail {
		defaults = append(defaults,
			dnsRecord{Name: "mail", Type: "A", Value: ip},
			dnsRecord{Name: "@", Type: "MX", Prio: 10, Value: "mail." + domain},
			dnsRecord{Name: "@", Type: "TXT", Value: "v=spf1 mx a -all"})
	}
	for _, rec := range defaults {
		if _, err := a.DB.Exec(`INSERT INTO dns_records(zone_id, name, type, ttl, prio, value) VALUES(?,?,?,?,?,?)`, id, rec.Name, rec.Type, 3600, rec.Prio, rec.Value); err != nil {
			return err
		}
	}
	if err := a.applyZone(id); err != nil {
		_, _ = a.DB.Exec(`DELETE FROM dns_zones WHERE id=?`, id)
		_ = a.applyZoneList()
		return err
	}
	a.audit("dns.zone.created", domain)
	writeJSON(w, 201, map[string]int64{"id": id})
	return nil
}

func (a *App) deleteZone(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	z, err := a.loadZone(id)
	if err != nil {
		return err
	}
	if _, err := a.DB.Exec(`DELETE FROM dns_zones WHERE id=?`, id); err != nil {
		return err
	}
	if err := a.applyZoneList(); err != nil {
		return err
	}
	_ = a.Sys.Remove(bindDir + "/db." + z.Domain)
	if _, err := a.Sys.Run("", "rndc", "reconfig"); err != nil {
		return bad("%v", err)
	}
	a.audit("dns.zone.deleted", z.Domain)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

// checkConflicts enforces the rule that a CNAME cannot share a name with
// any other record.
func checkConflicts(z dnsZone, rec dnsRecord) error {
	for _, other := range z.Records {
		if other.ID == rec.ID || other.Name != rec.Name {
			continue
		}
		if rec.Type == "CNAME" || other.Type == "CNAME" {
			return conflict("%s already has a %s record; a CNAME cannot coexist with other records", rec.Name, other.Type)
		}
		if other.Type == rec.Type && other.Value == rec.Value && other.Prio == rec.Prio {
			return conflict("an identical record already exists")
		}
	}
	return nil
}

func (a *App) createRecord(w http.ResponseWriter, r *http.Request) error {
	zoneID, err := pathID(r)
	if err != nil {
		return err
	}
	var rec dnsRecord
	if err := readJSON(r, &rec); err != nil {
		return err
	}
	rec.ID = 0
	a.mu.Lock()
	defer a.mu.Unlock()
	z, err := a.loadZone(zoneID)
	if err != nil {
		return err
	}
	if err := normalizeRecord(z.Domain, &rec); err != nil {
		return err
	}
	if err := checkConflicts(z, rec); err != nil {
		return err
	}
	res, err := a.DB.Exec(`INSERT INTO dns_records(zone_id, name, type, ttl, prio, value) VALUES(?,?,?,?,?,?)`, zoneID, rec.Name, rec.Type, rec.TTL, rec.Prio, rec.Value)
	if err != nil {
		return err
	}
	rec.ID, _ = res.LastInsertId()
	if err := a.applyZone(zoneID); err != nil {
		_, _ = a.DB.Exec(`DELETE FROM dns_records WHERE id=?`, rec.ID)
		return err
	}
	a.audit("dns.record.created", fmt.Sprintf("%s %s in %s", rec.Type, rec.Name, z.Domain))
	writeJSON(w, 201, rec)
	return nil
}

func (a *App) updateRecord(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var rec dnsRecord
	if err := readJSON(r, &rec); err != nil {
		return err
	}
	rec.ID = id
	a.mu.Lock()
	defer a.mu.Unlock()
	var zoneID int64
	var old dnsRecord
	if a.DB.QueryRow(`SELECT zone_id, name, type, ttl, prio, value FROM dns_records WHERE id=?`, id).Scan(&zoneID, &old.Name, &old.Type, &old.TTL, &old.Prio, &old.Value) != nil {
		return notFound("record")
	}
	z, err := a.loadZone(zoneID)
	if err != nil {
		return err
	}
	if err := normalizeRecord(z.Domain, &rec); err != nil {
		return err
	}
	if err := checkConflicts(z, rec); err != nil {
		return err
	}
	const q = `UPDATE dns_records SET name=?, type=?, ttl=?, prio=?, value=? WHERE id=?`
	if _, err := a.DB.Exec(q, rec.Name, rec.Type, rec.TTL, rec.Prio, rec.Value, id); err != nil {
		return err
	}
	if err := a.applyZone(zoneID); err != nil {
		_, _ = a.DB.Exec(q, old.Name, old.Type, old.TTL, old.Prio, old.Value, id)
		return err
	}
	a.audit("dns.record.updated", fmt.Sprintf("%s %s in %s", rec.Type, rec.Name, z.Domain))
	writeJSON(w, 200, rec)
	return nil
}

func (a *App) deleteRecord(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var zoneID int64
	var old dnsRecord
	if a.DB.QueryRow(`SELECT zone_id, name, type, ttl, prio, value FROM dns_records WHERE id=?`, id).Scan(&zoneID, &old.Name, &old.Type, &old.TTL, &old.Prio, &old.Value) != nil {
		return notFound("record")
	}
	if _, err := a.DB.Exec(`DELETE FROM dns_records WHERE id=?`, id); err != nil {
		return err
	}
	if err := a.applyZone(zoneID); err != nil {
		_, _ = a.DB.Exec(`INSERT INTO dns_records(id, zone_id, name, type, ttl, prio, value) VALUES(?,?,?,?,?,?,?)`, id, zoneID, old.Name, old.Type, old.TTL, old.Prio, old.Value)
		return err
	}
	a.audit("dns.record.deleted", fmt.Sprintf("%s %s", old.Type, old.Name))
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
