package app

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

const cronFile = "/etc/cron.d/hmea-panel"

var (
	reCronField  = regexp.MustCompile(`^[0-9*/,-]{1,40}$`)
	reCronPreset = regexp.MustCompile(`^@(hourly|daily|weekly|monthly|yearly|reboot)$`)
)

type cronJob struct {
	ID       int64  `json:"id"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Comment  string `json:"comment"`
	Enabled  bool   `json:"enabled"`
}

func validSchedule(s string) bool {
	if reCronPreset.MatchString(s) {
		return true
	}
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return false
	}
	for _, f := range fields {
		if !reCronField.MatchString(f) {
			return false
		}
	}
	return true
}

func oneLine(s string, max int) bool {
	if len(s) > max {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (j *cronJob) normalize() error {
	j.Schedule = strings.Join(strings.Fields(j.Schedule), " ")
	j.Command = strings.TrimSpace(j.Command)
	j.Comment = strings.TrimSpace(j.Comment)
	if !validSchedule(j.Schedule) {
		return bad("schedule must be five cron fields (minute hour day month weekday) or a preset such as @daily")
	}
	if j.Command == "" || !oneLine(j.Command, 1000) {
		return bad("command must be a single line of at most 1000 characters")
	}
	if !oneLine(j.Comment, 200) {
		return bad("description must be a single line of at most 200 characters")
	}
	return nil
}

func (a *App) loadCron() ([]cronJob, error) {
	rows, err := a.DB.Query(`SELECT id, schedule, command, comment, enabled FROM cron ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []cronJob{}
	for rows.Next() {
		var j cronJob
		if err := rows.Scan(&j.ID, &j.Schedule, &j.Command, &j.Comment, &j.Enabled); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// applyCron rewrites the cron.d file. Jobs run as www-data, never as root.
func (a *App) applyCron() error {
	jobs, err := a.loadCron()
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Managed by hmea-panel. Changes made here are overwritten.\nSHELL=/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\nMAILTO=\"\"\n")
	for _, j := range jobs {
		if j.Comment != "" {
			fmt.Fprintf(&b, "# %s\n", j.Comment)
		}
		prefix := ""
		if !j.Enabled {
			prefix = "#disabled# "
		}
		fmt.Fprintf(&b, "%s%s www-data %s\n", prefix, j.Schedule, j.Command)
	}
	return a.Sys.WriteFile(cronFile, []byte(b.String()), 0o644)
}

func (a *App) listCron(w http.ResponseWriter, r *http.Request) error {
	jobs, err := a.loadCron()
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"jobs": jobs})
	return nil
}

func (a *App) createCron(w http.ResponseWriter, r *http.Request) error {
	j := cronJob{Enabled: true}
	if err := readJSON(r, &j); err != nil {
		return err
	}
	if err := j.normalize(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.DB.Exec(`INSERT INTO cron(schedule, command, comment, enabled) VALUES(?,?,?,?)`, j.Schedule, j.Command, j.Comment, j.Enabled); err != nil {
		return err
	}
	if err := a.applyCron(); err != nil {
		return err
	}
	a.audit("cron.created", j.Schedule+" "+j.Command)
	writeJSON(w, 201, map[string]bool{"ok": true})
	return nil
}

func (a *App) updateCron(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var j cronJob
	if err := readJSON(r, &j); err != nil {
		return err
	}
	if err := j.normalize(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	res, err := a.DB.Exec(`UPDATE cron SET schedule=?, command=?, comment=?, enabled=? WHERE id=?`, j.Schedule, j.Command, j.Comment, j.Enabled, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("cron job")
	}
	if err := a.applyCron(); err != nil {
		return err
	}
	a.audit("cron.updated", j.Schedule+" "+j.Command)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) deleteCron(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	res, err := a.DB.Exec(`DELETE FROM cron WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("cron job")
	}
	if err := a.applyCron(); err != nil {
		return err
	}
	a.audit("cron.deleted", fmt.Sprint(id))
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
